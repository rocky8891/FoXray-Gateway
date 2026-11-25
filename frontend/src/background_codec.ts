export type BackgroundFiles = { image: Blob; preview: Blob };
const text = (bytes: Uint8Array) => String.fromCharCode(...bytes);
const tag = (bytes: Uint8Array, offset: number, value: string) =>
  [...value].every((char, index) => bytes[offset + index] === char.charCodeAt(0));

async function pngProfile(bytes: Uint8Array, view: DataView) {
  for (let offset = 8; offset + 12 <= bytes.length;) {
    const length = view.getUint32(offset);
    if (length > bytes.length - offset - 12) break;
    if (tag(bytes, offset + 4, "acTL")) throw new Error("请选择静态图片");
    if (tag(bytes, offset + 4, "iCCP")) {
      const chunk = bytes.slice(offset + 8, offset + 8 + length);
      const start = chunk.indexOf(0) + 2;
      if (start > 1 && start < chunk.length) {
        const stream = new Blob([chunk.slice(start)]).stream().pipeThrough(new DecompressionStream("deflate"));
        return new Uint8Array(await new Response(stream).arrayBuffer());
      }
    }
    if (tag(bytes, offset + 4, "IDAT")) break;
    offset += length + 12;
  }
}

function jpegProfile(bytes: Uint8Array, view: DataView) {
  const parts: Uint8Array[] = [];
  let count = 0;
  for (let offset = 2; offset + 4 <= bytes.length && bytes[offset] === 255;) {
    while (bytes[offset + 1] === 255) offset++;
    const marker = bytes[offset + 1];
    if (marker === 0xda || marker === 0xd9 || offset + 4 > bytes.length) break;
    const length = view.getUint16(offset + 2);
    if (length < 2 || offset + 2 + length > bytes.length) break;
    if (marker === 0xe2 && length >= 16 && tag(bytes, offset + 4, "ICC_PROFILE\0")) {
      const sequence = bytes[offset + 16];
      count = bytes[offset + 17];
      if (sequence && sequence <= count) parts[sequence - 1] = bytes.slice(offset + 18, offset + 2 + length);
    }
    offset += length + 2;
  }
  if (!count || Array.from({ length: count }, (_, index) => parts[index]).some(part => !part)) return;
  const profile = new Uint8Array(parts.reduce((total, part) => total + part.length, 0));
  let offset = 0;
  for (const part of parts) { profile.set(part, offset); offset += part.length; }
  return profile;
}

function webpProfile(bytes: Uint8Array, view: DataView) {
  let icc: Uint8Array | undefined;
  let lossless = false;
  for (let offset = 12; offset + 8 <= bytes.length;) {
    const length = view.getUint32(offset + 4, true);
    if (length > bytes.length - offset - 8) break;
    const kind = text(bytes.subarray(offset, offset + 4));
    if (kind === "ANIM" || kind === "ANMF") throw new Error("请选择静态图片");
    if (kind === "ICCP") icc = bytes.slice(offset + 8, offset + 8 + length);
    if (kind === "VP8L") lossless = true;
    offset += 8 + length + (length % 2);
  }
  return { icc, lossless };
}

function withProfile(encoded: ArrayBuffer, pixels: ImageData, icc?: Uint8Array) {
  if (!icc?.length) return new Blob([encoded], { type: "image/webp" });
  const raw = new Uint8Array(encoded);
  const header = new Uint8Array(38);
  const view = new DataView(header.buffer);
  const encoder = new TextEncoder();
  const padding = icc.length % 2;
  header.set(encoder.encode("RIFF"));
  view.setUint32(4, header.length + icc.length + padding + raw.length - 20, true);
  header.set(encoder.encode("WEBPVP8X"), 8);
  view.setUint32(16, 10, true);
  header[20] = 0x20 | ((raw[24] & 0x10) ? 0x10 : 0);
  view.setUint32(24, pixels.width - 1, true);
  view.setUint32(27, pixels.height - 1, true);
  header.set(encoder.encode("ICCP"), 30);
  view.setUint32(34, icc.length, true);
  return new Blob([header, new Uint8Array(icc).buffer, new Uint8Array(padding), raw.slice(12)], { type: "image/webp" });
}

export async function encodeBackground(file: File): Promise<BackgroundFiles> {
  const input = await file.arrayBuffer();
  const bytes = new Uint8Array(input);
  const view = new DataView(input);
  let pixels: ImageData;
  let icc: Uint8Array | undefined;
  let existingLossless = false;
  if (tag(bytes, 0, "\x89PNG\r\n\x1a\n")) {
    if (bytes[24] > 8) throw new Error("16 位 PNG 无法无损转换成 WebP，请使用 8 位原图");
    icc = await pngProfile(bytes, view);
    pixels = await (await import("@jsquash/png/decode.js")).default(input);
  } else if (bytes[0] === 0xff && bytes[1] === 0xd8) {
    icc = jpegProfile(bytes, view);
    if (icc && !tag(icc, 16, "RGB ")) throw new Error("请选择 RGB 色彩空间的图片");
    pixels = await (await import("@jsquash/jpeg/decode.js")).default(input);
  } else if (tag(bytes, 0, "RIFF") && tag(bytes, 8, "WEBP")) {
    ({ icc, lossless: existingLossless } = webpProfile(bytes, view));
    pixels = await (await import("@jsquash/webp/decode.js")).default(input);
  } else {
    throw new Error("请选择有效的 JPG、PNG 或 WebP 图片");
  }
  if (icc && !tag(icc, 16, "RGB ")) throw new Error("请选择 RGB 色彩空间的图片");
  const image = existingLossless ? file : withProfile(await (await import("@jsquash/webp/encode.js")).default(pixels, {
    lossless: 1, exact: 1, near_lossless: 100, method: 3,
  }), pixels, icc);
  const bitmap = await createImageBitmap(image);
  try {
    const scale = Math.min(1, 640 / Math.max(bitmap.width, bitmap.height));
    const canvas = new OffscreenCanvas(Math.max(1, Math.round(bitmap.width * scale)), Math.max(1, Math.round(bitmap.height * scale)));
    const context = canvas.getContext("2d");
    if (!context) throw new Error("无法生成背景预览");
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    return { image, preview: await canvas.convertToBlob({ type: "image/webp", quality: 0.75 }) };
  } finally {
    bitmap.close();
  }
}

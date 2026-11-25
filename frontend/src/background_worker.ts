import { encodeBackground } from "./background_codec";

self.onmessage = async (event: MessageEvent<File>) => {
  try {
    self.postMessage({ files: await encodeBackground(event.data) });
  } catch (error) {
    self.postMessage({ error: error instanceof Error ? error.message : "无法转换图片" });
  }
};

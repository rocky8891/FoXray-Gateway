import { readdirSync, readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
const literal = (text) => /border(?:-[a-z]+)*-radius\s*:[^;}]*\d(?:px|%)/i.test(text) || /borderRadius\s*:\s*(?:[1-9]\d*|["'`][^"'`]*\d(?:px|%))/i.test(text);
if (process.argv.includes('--self-test')) {
  for (const [source, expected] of [['a{border-radius:8px}',true],['a{border-radius:0}',false],['a{border-radius:var(--radius)}',false],['a{border-radius:var(--radius) 0 0}',false],['{borderRadius:8}',true],['{borderRadius:"var(--radius)"}',false],['a{border-top-left-radius:4px}',true]]) if (literal(source)!==expected) throw Error('Radius guard self-test failed');
  console.log('Radius guard self-test passed');
}
const root = resolve(dirname(fileURLToPath(import.meta.url)), '../src');
const failures=[];
for (const name of readdirSync(root,{recursive:true})) if (/\.(css|tsx)$/.test(name)) {
  const text=readFileSync(resolve(root,name),'utf8');
  text.split('\n').forEach((line,index)=>{if(literal(line))failures.push(`${name}:${index+1}`);});
}
if(failures.length) throw Error('Use radius tokens instead of literal radii: '+failures.join(', '));
console.log('Radius tokens verified');

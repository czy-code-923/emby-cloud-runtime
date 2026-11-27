const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const {test}=require('node:test');
const ts=require('typescript');
function load(name){if(name==="api-request")return {API_BASE:""};const output={};const code=ts.transpileModule(fs.readFileSync(path.join(__dirname,`../src/lib/${name}.ts`),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText;vm.runInNewContext(code,{exports:output,URL,require:(name)=>load(name.replace('./',''))});return output;}
const {commerceImageUrl}=load('commerce-images');
test('managed payment QR previews stay on the active entrance under self-only CSP',()=>{
 const file='/api/v2/commerce/assets/0123456789abcdef.png';
 assert.equal(commerceImageUrl(`https://shop.example.test${file}`),file);
 assert.equal(commerceImageUrl(file),file);
 assert.equal(commerceImageUrl('javascript:alert(1)'),'');
 assert.equal(commerceImageUrl('data:text/html,private'),'');
 assert.notEqual(commerceImageUrl('https://other.example.test/unrelated.jpg'),'/unrelated.jpg');
 assert.notEqual(commerceImageUrl(`${file}?redirect=1`),file);
});

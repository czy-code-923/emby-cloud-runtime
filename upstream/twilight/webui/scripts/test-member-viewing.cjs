const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const {test}=require('node:test');
const ts=require('typescript');
const exportsForTest={};
vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(__dirname,'../src/lib/member-viewing.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText,{exports:exportsForTest,Intl,Date});
const {viewingDurationParts,viewingTimestamp}=exportsForTest;
test('viewing duration preserves confirmed seconds without rounding or invented defaults',()=>{
 for(const [seconds,h,m,s] of [[0,0,0,0],[59,0,0,59],[60,0,1,0],[3661,1,1,1],[86401,24,0,1]]){
  assert.equal(JSON.stringify(viewingDurationParts(seconds)),JSON.stringify({hours:h,minutes:m,seconds:s}));
 }
 for(const value of [-1,NaN,Infinity,1.5,undefined,null,'60'])assert.equal(viewingDurationParts(value),null);
});
test('last viewing uses the recorded timestamp in Beijing time, never now or a future placeholder',()=>{
 const at=Date.parse('2026-11-27T16:01:02Z')/1000;
 assert.match(viewingTimestamp(at,at+30,'zh-Hans'),/2026.*11.*28.*00:01:02/);
 for(const value of [0,-1,at+31,NaN,undefined])assert.equal(viewingTimestamp(value,at+30),null);
});

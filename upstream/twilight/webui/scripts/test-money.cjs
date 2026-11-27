const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const {test}=require('node:test');
const ts=require('typescript');
const exportsForTest={};
vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(__dirname,'../src/lib/money.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText,{exports:exportsForTest});
const {yuanToCents,centsToYuan}=exportsForTest;
test('CNY preserves decimal cents exactly through product and receipt entry',()=>{
 for(const [text,cents] of [['19.99',1999],['0.01',1],['1.1',110],['0',0],['100000.00',10000000],[' 29.90 ',2990]]){
  assert.equal(yuanToCents(text),cents);assert.equal(yuanToCents(centsToYuan(cents)),cents);
 }
});
test('money rejects rounding, exponents, negative values and over-limit totals',()=>{
 for(const text of ['19.999','-1','1e3','Infinity','','1.','100000.01','9999999','0x10'])assert.equal(yuanToCents(text),null,text);
});

const statusExports={};
vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(__dirname,'../src/lib/media-status.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText,{exports:statusExports});
test('worker failure remains a failure in member and administrator projections',()=>{
 for(const input of ['failed','FAILED',' failed ']){assert.equal(statusExports.normalizeMediaRequestStatus(input,'admin'),'failed');assert.equal(statusExports.normalizeMediaRequestStatus(input,'user'),'FAILED')}
});

const calendarExports={};
vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(__dirname,'../src/lib/signin-calendar.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText,{exports:calendarExports,Intl,Date,Map});
test('30 check-in cells use Shanghai midnight and preserve zero credited check-ins',()=>{
 const cells=calendarExports.signinCalendar([{date:'2026-11-27',total:0}],new Date('2026-11-26T16:01:00Z'));
 assert.equal(cells.length,30);assert.equal(cells[29].date,'2026-11-27');assert.equal(cells[29].signed,true);assert.equal(cells[0].date,'2026-10-29');
});

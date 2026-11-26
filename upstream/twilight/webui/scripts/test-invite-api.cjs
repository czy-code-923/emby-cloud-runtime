const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const {test} = require('node:test');
const ts = require('typescript');
function client(response) {
 const calls=[], exported={};
 const source=fs.readFileSync(path.join(__dirname,'../src/lib/api.ts'),'utf8');
 const compiled=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText;
 vm.runInNewContext(compiled,{exports:exported,URL,URLSearchParams,AbortController,Date,encodeURIComponent,
  require(name) {
   if(name==='./api-request')return {API_BASE:'',DEFAULT_API_VERSION:'v2',apiRequest:async (...args)=>{calls.push(args);return response;}};
   if(name==='./deep-clone')return {deepClone:structuredClone};
   return {};
  }
 });
 return {api:exported.api,calls};
}
test('native tree preserves empty rows and forwards bounded server filters and selection',async()=>{
 const item={rows:[],selected:null,roots:[],page:1,pages:1,total_nodes:0,total_relations:0,total_rows:0};
 const {api,calls}=client({success:true,message:'OK',data:{item}});
 const controller=new AbortController();
 const response=await api.adminGetInviteTree(controller.signal,{page:2,search:'child',root:3,collapsed:[3],selected:7});
 assert.equal(response.data,item);
 assert.equal(response.data.rows.length,0);
 const query=new URL(calls[0][0],'https://panel.example').searchParams;
 assert.equal(query.get('per_page'),'100');
 for(const [key,value] of Object.entries({page:'2',search:'child',root:'3',collapsed:'3',selected:'7'}))assert.equal(query.get(key),value);
 assert.equal(calls[0][1].signal,controller.signal);
 assert.equal(calls[0][2].cacheRead,false);
 assert.equal(calls[0][2].dedupe,false);
});
test('personal invite summary has a single cancellable authenticated read',async()=>{
 const {api,calls}=client({success:true,message:'OK',data:{config:{enabled:true},invite:{codes:[],children:[]}}});
 const controller=new AbortController();
 const response=await api.getInviteSummary(controller.signal);
 assert.equal(calls.length,1);
 assert.equal(calls[0][0],'/invite/summary');
 assert.equal(calls[0][1].signal,controller.signal);
 assert.equal(response.data.invite.codes.length,0);
});

test('user quota update preserves success for the shared Go flat response',async()=>{
 const user={uid:2,username:'member',role:2,media_max_devices:5,media_max_streams:2,emby_sync_failed:true};
 for(const data of [user,{user,emby_sync_failed:true}]) {
  const {api,calls}=client({success:true,message:'user updated',data});
  const result=await api.updateUser(2,{media_max_devices:5,media_max_streams:2});
  assert.equal(result.success,true);
  assert.equal(result.data.uid,2);
  assert.equal(result.data.media_max_devices,5);
  assert.equal(result.data.emby_sync_failed,true);
  assert.equal(calls[0][0],'/admin/users/2');
  assert.equal(calls[0][1].method,'PUT');
 }
 const {api}=client({success:true,message:'unexpected response',data:{uid:3,username:'other'}});
 assert.equal((await api.updateUser(2,{media_max_streams:2})).success,false);
});

// Isolated Firefox UI fixtures only. No production session, playback, points or
// sign-in records are created by this test. Screenshots are labelled fixtures.
const {firefox}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const messages=require('../src/locales/zh-Hans.json');
const baseURL=process.env.WEBUI_TEST_URL||'http://127.0.0.1:3100';
const output=process.env.WEBUI_TEST_OUTPUT||path.join(__dirname,'../test-results/mini-viewing');
const asOf=Date.parse('2026-11-28T01:00:00Z')/1000;
const last=Date.parse('2026-11-27T16:01:02Z')/1000;
const positive={available:true,window_start:asOf-30*86400,window_end:asOf,seconds:3661,has_window_records:true,last_watched_at:last,last_poll:asOf-30,quality:'server_session_samples',source:'emby_server_session_samples'};

(async()=>{
 fs.mkdirSync(output,{recursive:true});
 const browser=await firefox.launch({headless:true});
 const results=[];
 try{
  for(const width of [360,768,1280]){
   let signed=false,points=37,viewing={...positive};
   const context=await browser.newContext({viewport:{width,height:900},locale:'zh-CN'});
   const page=await context.newPage();
   const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.route('https://telegram.org/js/telegram-web-app.js',route=>route.fulfill({contentType:'application/javascript',body:'window.Telegram={WebApp:{initData:"fixture-only",ready(){},expand(){}}};'}));
   await page.route('**/api/**',async route=>{
    const url=new URL(route.request().url());let data={};
    if(url.pathname.endsWith('/miniapp/session'))data={authenticated:true};
    else if(url.pathname.endsWith('/miniapp/account'))data={uid:42,username:'测试账号（样例数据）',emby_username:'fixture',role:0,role_label:'管理员',media_enabled:true,has_emby:true,expired_at:-1,points,signin_enabled:true,can_register:false,security_code_set:true,email_enabled:false,email_verified:false,email_masked:'',password_email_required:false,invite_enabled:false,requests_enabled:false,shop_url:'',banner:'',banner_url:'',viewing,signin:{summary:{today_signed:signed,current_points:points,renewal:{enabled:false}},config:{currency_name:'微光币',daily_min:18,daily_max:30},history:[]}};
    else if(url.pathname.endsWith('/miniapp/signin')){signed=true;points=57;}
    else if(url.pathname.endsWith('/miniapp/service'))data={online_users:0,watching_users:0,playing_sessions:0};
    else if(url.pathname.endsWith('/miniapp/retention'))data={account:{member:{permanent:true}}};
    else if(url.pathname.endsWith('/member-services'))data={items:[]};
    else if(url.pathname.endsWith('/device-registry'))data={items:[],max_devices:3,max_streams:1,playing_streams:0,sessions_available:true};
    else if(url.pathname.endsWith('/miniapp/lines'))data={lines:[],whitelist_lines:[]};
    else if(url.pathname.endsWith('/system/info'))data={name:'见微 UI 样例',features:{},setup:{available:false}};
    else if(url.pathname.endsWith('/auth/me'))data={uid:42,username:'fixture',role:0,active:true};
    await route.fulfill({contentType:'application/json',body:JSON.stringify({success:true,message:'OK',data})});
   });
   await page.goto(baseURL+'/miniapp');
   const nav=page.getByRole('navigation',{name:messages.mini.navigation});
   await nav.waitFor({timeout:90000});
   await nav.getByRole('button',{name:messages.mini.account,exact:true}).click();
   await page.getByText('1小时1分1秒',{exact:true}).waitFor();
   assert.ok((await page.locator('main').innerText()).includes('00:01:02'));
   assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
   await page.screenshot({path:path.join(output,`fixture-account-${width}.jpg`),fullPage:true});
   await nav.getByRole('button',{name:messages.v6.activityNav,exact:true}).click();
   const balance=page.getByText(messages.viewing.availablePoints,{exact:true}).locator('..');
   assert.match(await balance.innerText(),/37/);
   const signin=page.getByRole('button',{name:messages.mini.signin,exact:true});
   const a=await signin.boundingBox(),b=await balance.boundingBox();
   assert.ok(Math.abs((a.y+a.height/2)-(b.y+b.height/2))<12,'points are not beside the sign-in action');
   await signin.click();
   await page.getByRole('button',{name:messages.mini.signed,exact:true}).waitFor();
   assert.match(await balance.innerText(),/57/);
   assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
   await page.screenshot({path:path.join(output,`fixture-signin-${width}.jpg`),fullPage:true});
   viewing={...positive,seconds:0,has_window_records:false,last_watched_at:0,last_poll:0,quality:'waiting'};
   await page.reload();await nav.waitFor();
   await nav.getByRole('button',{name:messages.mini.account,exact:true}).click();
   await page.getByText(messages.viewing.noWindowRecords,{exact:true}).waitFor();
   await page.getByText(messages.viewing.noRecords,{exact:true}).waitFor();
   assert.deepEqual(errors,[]);
   results.push({width,exact_seconds:true,beijing_timestamp:true,no_fabricated_empty_state:true,points_adjacent_and_refreshed:true,no_horizontal_overflow:true});
   await context.close();
  }
 }finally{await browser.close();}
 fs.writeFileSync(path.join(output,'verification.json'),JSON.stringify({fixture_only:true,results,passed:true},null,2));
 console.log(JSON.stringify({fixture_only:true,results,passed:true}));
})().catch(e=>{console.error(e);process.exitCode=1;});

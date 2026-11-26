import fs from "node:fs";
import {createHash} from "node:crypto";
import path from "node:path";
import ts from "typescript-ast";
const root=path.resolve(import.meta.dirname,"../src");
const entries=[];const unresolved=[];
const category=(name,title="",body="")=>{
 const text=title+" "+body;
 if(name==="help_center.tsx"||/自动列宽|列筛选/.test(title))return "通用操作";
 if(/日志|任务中心|切换记录/.test(title)||/log_|task_/.test(name))return "日志与任务";
 if(/SELinux|系统权限|服务管理/.test(title))return "系统环境";
 if(/备份|回退|升级|版本信息/.test(title)||/config_backup|version_panel/.test(name))return "备份与升级";
 if(/HTTPS|认证|登录|密码|证书/.test(title)||/management_security/.test(name))return "账号与 HTTPS";
 if(/DNS|DDNS|DuckDNS/.test(title)||/^(dns|management_ddns)/.test(name))return "DNS 与 DDNS";
 if(/presentation|page_background|overview_|appearance|home_|theme/.test(name))return "首页与外观";
 if(/firewall|snat|dnat|cross_zone/.test(name))return "防火墙";
 if(/wireguard|^wg_/.test(name))return "WireGuard";
 if(/warp/.test(name))return "WARP";
 if(/^(network_hub|routing|interfaces)/.test(name))return "网络与路由";
 if(name==="network.tsx")return /WireGuard|Peer|AllowedIPs|Endpoint|密钥|公钥|隧道|接入角色|LAN|接口角色|备注|MTU/.test(text)?"WireGuard":"网络与路由";
 if(/monitor|traffic|metric|host|history_interval|connection|data_policy/.test(name))return "监控与统计";
 if(/nodes|custom_rules|xray_|vpn_exclusions|settings_save/.test(name))return "Xray";
 if(/路由探测|fwmark|ip_forward|默认网关|网络接口|IP地址|自动识别入口/.test(title))return "网络与路由";
 if(/检测|探测|监控|采样|统计|延迟|评分/.test(title)||/probe_/.test(name)||title==="字段说明"&&/真连接|自动选优/.test(body))return "监控与统计";
 if(/代理|节点|订阅|接管|自动切换|直连|绕过|配置与同步/.test(title)||/Xray/.test(text))return "Xray";
 if(/功能模块|Komari/.test(title))return "部署与安装";
 return "其他说明";
};
for(const file of fs.readdirSync(root).filter(name=>name.endsWith(".tsx")&&(!name.startsWith("help_")||name==="help_center.tsx")&&!name.includes("-check")).sort()){
 const source=ts.createSourceFile(file,fs.readFileSync(path.join(root,file),"utf8"),ts.ScriptTarget.Latest,true,ts.ScriptKind.TSX);
 const variables=new Map();
 function index(node){if(ts.isVariableDeclaration(node)&&ts.isIdentifier(node.name)&&node.initializer)variables.set(node.name.text,node.initializer);ts.forEachChild(node,index)}index(source);
 function plain(node,seen=new Set()){
  if(!node)return "";
  if(ts.isJsxText(node)||ts.isStringLiteral(node)||ts.isNoSubstitutionTemplateLiteral(node))return node.text.replace(/\s+/g," ");
  if(ts.isJsxExpression(node))return plain(node.expression,seen);
  if(ts.isIdentifier(node)){if(seen.has(node.text))return "";const next=new Set(seen);next.add(node.text);return plain(variables.get(node.text),next)}
  if(ts.isJsxElement(node)||ts.isJsxFragment(node))return node.children.map(child=>plain(child,seen)).join("").trim()+"\n";
  if(ts.isConditionalExpression(node))return plain(node.whenTrue,seen)+"\n"+plain(node.whenFalse,seen);
  if(ts.isBinaryExpression(node))return node.operatorToken.kind===ts.SyntaxKind.AmpersandAmpersandToken?plain(node.right,seen):plain(node.left,seen)+plain(node.right,seen);
  if(ts.isParenthesizedExpression(node)||ts.isAsExpression(node))return plain(node.expression,seen);
  if(ts.isArrayLiteralExpression(node))return node.elements.map(child=>plain(child,seen)).join("\n");
  if(ts.isTemplateExpression(node))return node.head.text+node.templateSpans.map(span=>"（当前值）"+span.literal.text).join("");
  if(ts.isCallExpression(node)){return node.arguments.map(arg=>plain(arg,seen)).join(" ")}
  if(ts.isArrowFunction(node))return plain(node.body,seen);
  return "";
 }
 function visit(node){
  if(ts.isJsxOpeningElement(node)||ts.isJsxSelfClosingElement(node)){
   const tag=node.tagName.getText(source),attrs=new Map(node.attributes.properties.filter(ts.isJsxAttribute).map(a=>[a.name.getText(source),a.initializer]));
   let body="",title="";
   if(tag==="SettingInfo"){title=plain(attrs.get("label"));body=plain(attrs.get("text"))}
   if(tag==="Field"&&(attrs.has("hint")||attrs.has("info"))){title=plain(attrs.get("label"));body=[plain(attrs.get("hint")),plain(attrs.get("info"))].filter(Boolean).join("\n")}
   if(tag!=="SettingInfo"&&tag!=="Field"&&attrs.has("info")){title=plain(attrs.get("title")||attrs.get("label"));body=plain(attrs.get("info"))}
   if(tag==="PageHeader"&&attrs.has("description")){title=plain(attrs.get("title"));body=plain(attrs.get("description"))}
   if(body&&!title&&tag==="Field")title="字段说明";
   body=body.split("\n").map(line=>line.trim()).filter(Boolean).join("\n");title=title.replace(/\s+/g," ").trim();
   if(tag==="SettingInfo"&&(!body||!title))unresolved.push({file,label:attrs.get("label")?.getText(source),text:attrs.get("text")?.getText(source).slice(0,100)});
   if(body&&title)entries.push({id:"info-"+entries.length,title,category:category(file,title,body),body,origin:file.replace(/\.tsx$/,""),kind:"页面说明"});
  }
  ts.forEachChild(node,visit);
 }
 visit(source);
}
const unique=[...new Map(entries.map(item=>[item.title+"\n"+item.body,item])).values()].map((item,index)=>({...item,id:"info-"+createHash("sha256").update(item.title+"\n"+item.body).digest("hex").slice(0,16)}));
unique.push({id:"table-width",title:"自动列宽与手动调整",category:"通用操作",kind:"页面说明",origin:"通用表格",body:"自动列宽：按当前页内容计算；空间不足可横向滚动，默认最大 480 px。\n手动调整：拖动表头分隔线，宽度保存在当前浏览器。\n恢复默认：双击分隔线恢复单列，点击自动列宽恢复整表。"});
fs.writeFileSync(path.join(root,"help_catalog.json"),JSON.stringify(unique,null,2)+"\n");
if(unresolved.some(item=>item.file!=="ui.tsx"))throw Error("Unresolved authored help: "+JSON.stringify(unresolved));
console.log(`Help catalog: ${unique.length} searchable page explanations`);

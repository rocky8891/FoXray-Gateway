import fs from "node:fs";
import {createHash} from "node:crypto";
import path from "node:path";
import ts from "typescript-ast";
const root=path.resolve(import.meta.dirname,"../src");
const entries=[];const unresolved=[];
const category=name=>/wireguard|wg_|network/.test(name)?"WireGuard 与网络":/firewall|snat|dnat|cross_zone/.test(name)?"防火墙":/warp/.test(name)?"WARP":/management|security|backup|version/.test(name)?"部署与管理":/monitor|traffic|metric|overview|host|history|connection/.test(name)?"监控与首页":"功能说明";
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
   if(body&&!title&&tag==="Field")title=category(file)+" · 字段说明";
   body=body.split("\n").map(line=>line.trim()).filter(Boolean).join("\n");title=title.replace(/\s+/g," ").trim();
   if(tag==="SettingInfo"&&(!body||!title))unresolved.push({file,label:attrs.get("label")?.getText(source),text:attrs.get("text")?.getText(source).slice(0,100)});
   if(body&&title)entries.push({id:"info-"+entries.length,title,category:category(file),body,origin:file.replace(/\.tsx$/,""),kind:"页面说明"});
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

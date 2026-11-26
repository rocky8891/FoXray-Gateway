import {useMemo,useState} from "react";
import {Search,BookOpen,Star} from "lucide-react";
import {Button,Panel,Modal} from "./ui";
import {CopyButton} from "./resource_controls";
import catalog from "./help_catalog.json";
import manuals from "./help_articles.json";
import "./help_center.css";
type Article={id:string;title:string;category:string;body:string;kind:string;origin:string;keywords?:string;common?:boolean};
const articles:Article[]=[...manuals,...catalog];
const normalize=(value:string)=>value.toLowerCase().normalize("NFKC").replace(/[\s_\-·，。:：/]+/g,"");
function fuzzy(needle:string,haystack:string){
 if(haystack.includes(needle))return 1;
 let at=0,start=-1;for(let i=0;i<haystack.length;i++){if(haystack[i]===needle[at]){if(start<0)start=i;at++;if(at===needle.length){if(i-start<=needle.length*3)return .65;at=0;start=-1;}}}
 if(needle.length<3)return 0;
 const pairs=new Set(Array.from({length:needle.length-1},(_,i)=>needle.slice(i,i+2)));
 const hits=[...pairs].filter(pair=>haystack.includes(pair)).length/pairs.size;
 return hits>=.5?hits*.5:0;
}
function score(article:Article,query:string){
 const parts=query.trim().split(/\s+/).map(normalize).filter(Boolean);
 if(!parts.length)return article.common?2:1;
 const title=normalize(article.title+" "+(article.keywords||"")),body=normalize(article.body);
 const ranks=parts.map(part=>Math.max(fuzzy(part,title)*3,fuzzy(part,body)));
 return ranks.some(rank=>rank===0)?0:ranks.reduce((sum,rank)=>sum+rank,0);
}
function Highlighted({text,query}:{text:string;query:string}){
 const tokens=query.trim().split(/\s+/).filter(Boolean);if(!tokens.length)return <>{text}</>;
 const escaped=tokens.map(token=>token.replace(/[.*+?^${}()|[\]\\]/g,"\\$&"));
 const expression=new RegExp("("+escaped.join("|")+")","ig");
 return <>{text.split(expression).map((part,index)=>tokens.some(token=>token.toLowerCase()===part.toLowerCase())?<mark key={index}>{part}</mark>:part)}</>;
}
function snippet(article:Article,query:string){
 const body=article.body.replace(/```[\s\S]*?```/g," ").replace(/## /g,"");
 const hits=query.trim().split(/\s+/).filter(Boolean).map(token=>body.toLowerCase().indexOf(token.toLowerCase())).filter(index=>index>=0);
 const start=hits.length?Math.max(0,Math.min(...hits)-35):0;
 return (start?"…":"")+body.slice(start,start+190);
}
function ArticleBody({body}:{body:string}){
 const parts=body.split("```");return <div className="help-article-body">{parts.map((part,index)=>index%2?<div className="help-code" key={index}><CopyButton text={part.replace(/^\w*\n/,"")}/><pre>{part.replace(/^\w*\n/,"")}</pre></div>:part.split("\n").filter(Boolean).map((line,i)=>line.startsWith("## ")?<h3 key={index+"-"+i}>{line.slice(3)}</h3>:<p key={index+"-"+i}>{line}</p>))}</div>;
}
export function HelpCenter(){
 const [query,setQuery]=useState(""),[category,setCategory]=useState("全部"),[kind,setKind]=useState("全部"),[limit,setLimit]=useState(30),[opened,setOpened]=useState<Article|null>(null);
 const [saved,setSaved]=useState<string[]>(()=>{try{const v=JSON.parse(localStorage.getItem("foxray.help.saved")||"[]");return Array.isArray(v)?v.filter(x=>typeof x==="string"):[];}catch{return []}});
 const categories=["全部",...new Set(articles.map(article=>article.category))];
 const results=useMemo(()=>articles.filter(article=>(category==="全部"||article.category===category)&&(kind==="全部"||article.kind===kind)).map(article=>({article,rank:score(article,query)})).filter(row=>row.rank>0).sort((a,b)=>b.rank-a.rank||a.article.title.localeCompare(b.article.title)),[query,category,kind]);
 const favorites=articles.filter(article=>saved.includes(article.id));
 const common=(favorites.length?favorites:manuals.slice(0,6)) as Article[];
 const save=(article:Article)=>{const next=saved.includes(article.id)?saved.filter(id=>id!==article.id):[...saved,article.id];setSaved(next);try{localStorage.setItem("foxray.help.saved",JSON.stringify(next));}catch{/* Collection still works for this session. */}};
 return <div className="help-center">
  <Panel><div className="help-heading"><BookOpen/><div><h2>帮助中心</h2><p>部署、功能使用与页面说明 · 支持关键词和模糊搜索</p></div></div>
   <label className="help-search"><Search size={20}/><input aria-label="搜索帮助" maxLength={200} placeholder="搜索部署、SELinux、ip_forward、WireGuard、自动列宽…" value={query} onChange={event=>{setQuery(event.target.value);setLimit(30)}}/>{query&&<Button onClick={()=>setQuery("")}>清空</Button>}</label>
  </Panel>
  {!query&&<Panel><h3>{favorites.length?"收藏文档":"常用文档"}</h3><div className="help-common">{common.map(article=><button key={article.id} onClick={()=>setOpened(article)}><BookOpen size={18}/>{article.title}</button>)}</div></Panel>}
  <div className="help-layout"><aside aria-label="帮助分类">{categories.map(item=><button key={item} aria-pressed={category===item} onClick={()=>{setCategory(item);setLimit(30)}}>{item}<small>{item==="全部"?articles.length:articles.filter(article=>article.category===item).length}</small></button>)}</aside>
   <Panel><div className="help-results-head"><span>{results.length} 篇匹配文档</span><select aria-label="帮助文档类型" value={kind} onChange={event=>{setKind(event.target.value);setLimit(30)}}><option>全部</option><option>使用手册</option><option>页面说明</option></select></div>
    {!results.length&&<p>未找到匹配内容，请减少关键词或切换分类。</p>}
    <div className="help-results">{results.slice(0,limit).map(({article})=><article key={article.id}><button className="help-result-title" onClick={()=>setOpened(article)}><Highlighted text={article.title} query={query}/></button><small>{article.category} · {article.kind}</small><p><Highlighted text={snippet(article,query)} query={query}/></p></article>)}</div>
    {results.length>limit&&<Button onClick={()=>setLimit(limit+30)}>显示更多</Button>}
   </Panel></div>
  {opened&&<Modal title={opened.title} wide onClose={()=>setOpened(null)}><div className="help-article-tools"><span>{opened.category} · {opened.kind}</span><Button onClick={()=>save(opened)}><Star size={16}/>{saved.includes(opened.id)?"取消收藏":"收藏文档"}</Button><CopyButton text={opened.title+"\n\n"+opened.body}/></div><ArticleBody body={opened.body}/></Modal>}
 </div>;
}

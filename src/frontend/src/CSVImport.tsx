import {useEffect, useRef, useState} from 'react'

type Preview = {digest:string; headers:string[]; types:string[]; rows:number; sample:string[][]; problems:{row:number; column:string; message:string}[]; problemCount:number}
const kinds = ['title','text','number','checkbox','date','url','email']
export default function CSVImport({onImported}:{onImported:()=>Promise<void>}) {
 const [source,setSource]=useState('')
 const [title,setTitle]=useState('Imported CSV')
 const [types,setTypes]=useState<string[]>([])
 const [headers,setHeaders]=useState<string[]>([])
 const [preview,setPreview]=useState<Preview|null>(null)
 const [busy,setBusy]=useState('')
 const [error,setError]=useState('')
 const [notice,setNotice]=useState('')
 const [complete,setComplete]=useState(false)
 const operation=useRef('')
 const active=useRef<AbortController|null>(null)
 useEffect(()=>()=>active.current?.abort(),[])
 const invalidate=()=>{setPreview(null);setComplete(false);setNotice('');setError('');operation.current=crypto.randomUUID()}
 const run=async(commit:boolean)=>{
  const controller=new AbortController();active.current=controller;setBusy(commit?'import':'preview');setError('');setNotice('')
  try {
   const response=await fetch('/api/import/csv'+(commit?'':'?preview=1'),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({csv:source,title,types,digest:commit?preview!.digest:undefined,operationKey:commit?operation.current:undefined}),signal:controller.signal})
   const data=await response.json()
   if(!response.ok)throw new Error(data.error||'CSV import failed.')
   if(commit){setComplete(true);setNotice(`Imported ${data.rows} records into a separate database named ${title}.`);await onImported()}
   else {const next=data as Preview;setPreview(next);setHeaders(next.headers);setTypes(next.types);setNotice(next.problemCount?'Fix validation problems or choose Text to preserve the original values, then preview again.':'Preview ready. Confirm the field types before creating a separate database.')}
  }catch(e){if(!controller.signal.aborted)setError((e as Error).message)}
  finally{if(!controller.signal.aborted){setBusy('');active.current=null}}
 }
 return <section className="archive-panel" aria-label="CSV database import" aria-busy={!!busy}>
  <div className="section-heading"><h3>Import a CSV database</h3><span>Preview, map and validate</span></div>
  <p>Create an independent database and Table view from one UTF-8, comma-separated CSV. Existing data stays unchanged. The first column starts as Title and others as Text; choose types explicitly, then preview again. No values or columns are silently skipped.</p>
  <p className="empty">Up to 2 MB, 1,000 data records, 32 uniquely named columns and 32 KB per cell. Exactly one non-blank Title column is required. Numbers use decimal notation and standard numeric precision (up to ±9,007,199,254,740,991); keep identifiers as Text. Checkboxes use true/false; dates use YYYY-MM-DD or RFC3339. Empty typed cells stay empty. Quoted commas/newlines are supported. Relations, formulas, select options, attachments, record documents and Notion ZIP mapping are not included.</p>
  <label className="archive-file-label">CSV file<input type="file" accept=".csv,text/csv" aria-label="CSV file" disabled={!!busy} onChange={async e=>{
   invalidate();setSource('');setHeaders([]);setTypes([])
   const file=e.target.files?.[0];if(!file)return
   if(file.size>2*1024*1024){setError('Choose a CSV no larger than 2 MB.');e.target.value='';return}
   setBusy('read')
   try{setSource(new TextDecoder('utf-8',{fatal:true}).decode(await file.arrayBuffer()))}catch{setError('Could not read a UTF-8 CSV. Convert the source to UTF-8 and retry.')}
   finally{setBusy('')}
  }}/></label>
  <label className="archive-file-label">New database title<input aria-label="New CSV database title" value={title} disabled={!!busy} onChange={e=>{invalidate();setTitle(e.target.value)}} /></label>
  {headers.length>0&&<fieldset className="csv-mapping" disabled={!!busy}><legend>Column types</legend>{headers.map((header,index)=><label key={index}>{header}<select aria-label={'Type for '+header} value={types[index]} onChange={e=>{invalidate();setTypes(current=>current.map((kind,i)=>i===index?e.target.value:kind))}}>{kinds.map(kind=><option value={kind} key={kind}>{kind==='url'?'URL':kind[0].toUpperCase()+kind.slice(1)}</option>)}</select></label>)}</fieldset>}
  <div className="row archive-actions"><button className="btn" disabled={!source||!title||!!busy||complete} onClick={()=>void run(false)}>{busy==='preview'?'Validating CSV…':'Preview CSV'}</button>{preview&&preview.problemCount===0&&<button className="btn primary" disabled={!!busy||complete} onClick={()=>void run(true)}>{busy==='import'?'Importing CSV…':'Import CSV copy'}</button>}</div>
  {preview&&<div className="archive-preview" aria-label="CSV preview"><p><strong>{preview.rows} records</strong> · {preview.headers.length} columns · {preview.problemCount} validation problems</p><p>First {preview.sample.length} source records, shown without type conversion. Row numbers count CSV records, including the header as row 1.</p><div className="csv-sample" tabIndex={0} role="region" aria-label="CSV sample rows"><table><caption>Source sample</caption><thead><tr>{preview.headers.map((header,i)=><th scope="col" key={i}>{header}</th>)}</tr></thead><tbody>{preview.sample.map((row,i)=><tr key={i}>{row.map((cell,j)=><td key={j}>{cell||'∅ (empty)'}</td>)}</tr>)}</tbody></table></div>{preview.problemCount>0&&<div role="alert"><p>{preview.problemCount} problems. Showing up to 100; no records have been imported.</p><ul>{preview.problems.map((problem,i)=><li key={i}>Row {problem.row}, {problem.column}: {problem.message}</li>)}</ul></div>}</div>}
  {notice&&<p role="status">{notice}</p>}{error&&<p role="alert">{error} If a successful import response was interrupted, retry without changing the source or mapping to retrieve the completed copy.</p>}
 </section>
}

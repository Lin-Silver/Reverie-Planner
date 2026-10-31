import {useCallback, useEffect, useState} from 'react'

export type Appearance = {style:'glass'|'classic';wallpaper:'sakura'|'mist';material:'soft'|'balanced'|'frosted';version:number}
const initial:Appearance={style:'glass',wallpaper:'sakura',material:'balanced',version:0}
export function useAppearance(){
 const [appearance,setAppearance]=useState<Appearance>(initial)
 const [loaded,setLoaded]=useState(false)
 const [saving,setSaving]=useState(false)
 const [error,setError]=useState('')
 const [notice,setNotice]=useState('')
 const [attempt,setAttempt]=useState(0)
 useEffect(()=>{
  const controller=new AbortController()
  fetch('/api/appearance',{signal:controller.signal}).then(async response=>{if(!response.ok)throw new Error('Appearance could not be loaded.');return response.json() as Promise<Appearance>}).then(value=>{setAppearance(value);setLoaded(true);setError('')}).catch(e=>{if(!controller.signal.aborted)setError((e as Error).message)})
  return()=>controller.abort()
 },[attempt])
 useEffect(()=>{
  const root=document.documentElement
  root.dataset.interface=appearance.style;root.dataset.wallpaper=appearance.wallpaper;root.dataset.material=appearance.material
 },[appearance])
 const save=useCallback(async(change:Partial<Appearance>)=>{
  if(!loaded||saving)return
  setSaving(true);setError('');setNotice('')
  try{
   const response=await fetch('/api/appearance',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({...appearance,...change})})
   const value=await response.json()
   if(!response.ok)throw new Error(value.error||'Appearance could not be saved.')
   setAppearance(value as Appearance);setNotice('Appearance saved in this workspace.')
  }catch(e){setError((e as Error).message)}finally{setSaving(false)}
 },[appearance,loaded,saving])
 const reload=()=>{setLoaded(false);setNotice('');setAttempt(value=>value+1)}
 return {appearance,loaded,saving,error,notice,save,reload}
}

import type {useAppearance} from './useAppearance'
import type {Appearance} from './useAppearance'

export default function AppearanceSettings({preferences}:{preferences:ReturnType<typeof useAppearance>}){
 const {appearance,loaded,saving,error,notice,save,reload}=preferences
 return <section className="appearance-panel" aria-label="Workspace appearance">
  <div className="section-heading"><div><span className="eyebrow">Make it yours</span><h3>Appearance</h3></div><span>Saved with your workspace</span></div>
  <p className="appearance-intro">A little more space. A softer point of view.</p>
  <fieldset disabled={!loaded||saving} className="appearance-options"><legend>Interface</legend><div className="appearance-segmented">
   <button type="button" aria-label="Glass interface" aria-pressed={appearance.style==='glass'} onClick={()=>void save({style:'glass'})}><span className="interface-thumbnail glass-thumbnail" aria-hidden="true"><i/><i/></span><strong>Glass</strong><small>Layered and translucent</small></button>
   <button type="button" aria-label="Classic interface" aria-pressed={appearance.style==='classic'} onClick={()=>void save({style:'classic'})}><span className="interface-thumbnail classic-thumbnail" aria-hidden="true"><i/><i/></span><strong>Classic</strong><small>A familiar workspace</small></button>
  </div></fieldset>
  <fieldset disabled={!loaded||saving||appearance.style==='classic'} className="appearance-options"><legend>Wallpaper</legend><div className="wallpaper-options">
   <button type="button" aria-label="Sakura wallpaper" aria-pressed={appearance.wallpaper==='sakura'} onClick={()=>void save({wallpaper:'sakura'})}><span className="wallpaper-swatch sakura-swatch" aria-hidden="true"/><span>Sakura</span></button>
   <button type="button" aria-label="Mist wallpaper" aria-pressed={appearance.wallpaper==='mist'} onClick={()=>void save({wallpaper:'mist'})}><span className="wallpaper-swatch mist-swatch" aria-hidden="true"/><span>Mist</span></button>
  </div><label className="appearance-material">Glass clarity<select aria-label="Glass clarity" value={appearance.material} onChange={e=>void save({material:e.target.value as Appearance['material']})}><option value="soft">Clear</option><option value="balanced">Balanced</option><option value="frosted">Frosted</option></select></label></fieldset>
  <p className="appearance-hint">Navigation uses a lighter glass. Writing and tables use a thicker reading material. Reduce Transparency and Increase Contrast use solid surfaces.</p>
  <p role="status" className="appearance-status">{saving?'Saving appearance…':notice||(!loaded&&!error?'Loading appearance…':'')}</p>
  {error&&<p role="alert">{error} Your previous appearance stays active.</p>}
  <button className="btn quiet" disabled={saving} onClick={reload}>Reload appearance</button>
 </section>
}

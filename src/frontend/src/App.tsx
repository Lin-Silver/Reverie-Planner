import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import PageEditor from './PageEditor'
import type { FormEvent } from 'react'
import './App.css'

type ProjectStatus = 'active' | 'planning' | 'paused' | 'archived'
type TaskStatus = 'todo' | 'in_progress' | 'done' | 'blocked'
type Priority = 'low' | 'medium' | 'high'
type Project = { id: string; name: string; description: string; status: ProjectStatus; owner: string; color: string; createdAt: string }
type Task = { id: string; projectId: string; title: string; description?: string; status: TaskStatus; priority: Priority; dueDate?: string; assignee?: string; createdAt: string; stateId?: string; cycleId?: string; moduleId?: string; estimate?: string; parentId?: string; labelsJson?: string }
type Overview = { projectCount: number; taskCount: number; completedCount: number; upcomingCount: number; aiEnabled: boolean }
type Provider = { id: string; label: string; provider: string; baseURL: string; model: string; temperature: number; maxTokens: number; enabled: boolean; streaming: boolean; toolCalling: boolean; apiKeyConfigured: boolean }
type TaskDraft = { projectId: string; title: string; description: string; status: TaskStatus; priority: Priority; dueDate: string; assignee: string }
type ChatMessage = { role: 'user' | 'assistant'; content: string; id: string }
type Page = { id: string; projectId?: string; workspace: boolean; parentId?: string; title: string; icon?: string; cover?: string; content: string; version?: number; createdBy: string; createdAt: string; updatedAt: string }
type Cycle = { id: string; projectId: string; name: string; description?: string; startDate?: string; endDate?: string; status: string; createdAt: string }
type Module = { id: string; projectId: string; name: string; description?: string; status: string; lead?: string; startDate?: string; endDate?: string; createdAt: string }
type Notification = { id: string; title: string; body?: string; type: string; read: boolean; createdAt: string }
type Sticky = { id: string; title: string; body: string; color: string; createdAt: string }
type Draft = { id: string; kind: string; title: string; content: string; createdAt: string; updatedAt: string }
type IntakeItem = { id: string; projectId: string; title: string; body?: string; status: string; createdAt: string }
type ChatResponse = { sessionId: string; response: string; pendingApprovals?: any[] }
type Approval = { id: string; sessionId: string; toolName: string; arguments: Record<string, unknown>; summary: string; highRisk: boolean; status: string }


async function apiFetch<T>(input: string, init?: RequestInit): Promise<T> {
  const response = await fetch(input, { ...init, headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) } })
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: 'Request failed' }))
    throw new Error((error as { error?: string }).error ?? 'Request failed')
  }
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

const uid = () => Math.random().toString(36).slice(2, 10)

export default function App() {
  const [theme, setTheme] = useState<'light' | 'dark'>(() => (localStorage.getItem('theme') as any) || 'light')
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => localStorage.getItem('reverie.sidebar.collapsed') === '1')
  const [view, setView] = useState<string>('home')
  const [selectedPage, setSelectedPage] = useState<string>('')
  const [selectedProject, setSelectedProject] = useState<string>('all')
  const [search, setSearch] = useState('')
  const [searchResults, setSearchResults] = useState<any>(null)
  const [error, setError] = useState('')

  const [projects, setProjects] = useState<Project[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [overview, setOverview] = useState<Overview | null>(null)
  const [providers, setProviders] = useState<Provider[]>([])
  const [pages, setPages] = useState<Page[]>([])
  const [cycles, setCycles] = useState<Cycle[]>([])
  const [modules, setModules] = useState<Module[]>([])
  const [intake, setIntake] = useState<IntakeItem[]>([])
  const [notifications, setNotifications] = useState<Notification[]>([])
  const [stickies, setStickies] = useState<Sticky[]>([])
  const [drafts, setDrafts] = useState<Draft[]>([])
  const [analytics, setAnalytics] = useState<any>(null)
  const [workspaces, setWorkspaces] = useState<any[]>([])
  const [members, setMembers] = useState<any[]>([])

  // dialogs
  const [taskEditorOpen, setTaskEditorOpen] = useState(false)
  const [editingTask, setEditingTask] = useState<Task | null>(null)
  const [taskDraft, setTaskDraft] = useState<TaskDraft>({ projectId: '', title: '', description: '', status: 'todo', priority: 'medium', dueDate: '', assignee: '' })
  const [projectEditorOpen, setProjectEditorOpen] = useState(false)
  const [editingProject, setEditingProject] = useState<Project | null>(null)
  const [projectDraft, setProjectDraft] = useState({ name: '', description: '', status: 'planning' as ProjectStatus, owner: 'You', color: 'cyan' })
  const [pageEditor, setPageEditor] = useState<Page | null>(null)
  const [pageDraft, setPageDraft] = useState({ title: 'Untitled', icon: '', content: '' })
  const [selectedProvider, setSelectedProvider] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [prompt, setPrompt] = useState('')
  const [approvals, setApprovals] = useState<Approval[]>([])
  const [dropTarget, setDropTarget] = useState<TaskStatus | ''>('')
  const draggedTaskId = useRef('')
  const pageEditorRef = useRef<Page | null>(null)
  const pageTitleRef = useRef('')
  useEffect(() => { pageEditorRef.current = pageEditor; pageTitleRef.current = pageDraft.title }, [pageEditor, pageDraft.title])
  const savePageContent = async (content: string) => {
    const pe = pageEditorRef.current
    if (!pe) return
    try {
      const res = await apiFetch<Page>('/api/pages', { method: 'PUT', body: JSON.stringify({ ...pe, title: pageTitleRef.current, icon: pe.icon, content, version: pe.version }) })
      if (pageEditorRef.current && res.version) pageEditorRef.current.version = res.version
    } catch (e) {
      // on conflict, reload latest
      if ((e as Error).message.includes('version conflict') && pe.id) {
        try {
          const latest = await apiFetch<Page>(`/api/pages?id=${pe.id}`)
          pageEditorRef.current = latest
          setPageEditor(latest)
        } catch {}
      }
      throw e
    }
  }

  useEffect(() => { document.documentElement.setAttribute('data-theme', theme); localStorage.setItem('theme', theme) }, [theme])
  useEffect(() => { localStorage.setItem('reverie.sidebar.collapsed', sidebarCollapsed ? '1' : '0') }, [sidebarCollapsed])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { setTaskEditorOpen(false); setProjectEditorOpen(false) } }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const refresh = async () => {
    try {
      const [pr, tk, ov, pv, pg, cy, mo, it, nt, sk, dr, an, ws, mb] = await Promise.all([
        apiFetch<Project[]>('/api/projects'),
        apiFetch<Task[]>('/api/tasks'),
        apiFetch<Overview>('/api/overview'),
        apiFetch<Provider[]>('/api/ai/providers'),
        apiFetch<Page[]>('/api/pages'),
        apiFetch<Cycle[]>('/api/cycles'),
        apiFetch<Module[]>('/api/modules'),
        apiFetch<IntakeItem[]>('/api/intake'),
        apiFetch<Notification[]>('/api/notifications'),
        apiFetch<Sticky[]>('/api/stickies'),
        apiFetch<Draft[]>('/api/drafts'),
        apiFetch<any>('/api/analytics'),
        apiFetch<any[]>('/api/workspaces'),
        apiFetch<any[]>('/api/members'),
      ])
      setProjects(pr); setTasks(tk); setOverview(ov); setProviders(pv)
      setPages(pg); setCycles(cy); setModules(mo); setIntake(it); setNotifications(nt); setStickies(sk); setDrafts(dr); setAnalytics(an); setWorkspaces(ws); setMembers(mb)
      setSelectedProvider((c) => c || pv.find((p) => p.enabled)?.id || pv[0]?.id || '')
      setError('')
    } catch (e) { setError((e as Error).message) }
  }
  useEffect(() => { void refresh() }, [])

  const visibleTasks = useMemo(() => tasks.filter((t) => (selectedProject === 'all' || t.projectId === selectedProject)), [tasks, selectedProject])

  const openProjectEditor = (project?: Project) => {
    setEditingProject(project ?? null)
    setProjectDraft(project ? { name: project.name, description: project.description, status: project.status, owner: project.owner, color: project.color } : { name: '', description: '', status: 'planning', owner: 'You', color: 'cyan' })
    setProjectEditorOpen(true)
  }
  const saveProject = async (e: FormEvent) => {
    e.preventDefault()
    if (!projectDraft.name.trim()) return
    try {
      await apiFetch('/api/projects', { method: editingProject ? 'PUT' : 'POST', body: JSON.stringify({ ...projectDraft, id: editingProject?.id }) })
      setProjectEditorOpen(false); await refresh()
    } catch (err) { setError((err as Error).message) }
  }
  const deleteProject = async (id: string) => {
    if (!confirm('Archive this project?')) return
    try { await apiFetch('/api/projects', { method: 'DELETE', body: JSON.stringify({ id }) }); await refresh() } catch (e) { setError((e as Error).message) }
  }

  const openTaskEditor = useCallback((task?: Task) => {
    setEditingTask(task ?? null)
    setTaskDraft(task ? { projectId: task.projectId, title: task.title, description: task.description ?? '', status: task.status, priority: task.priority, dueDate: task.dueDate ?? '', assignee: task.assignee ?? '' } : { projectId: projects[0]?.id ?? '', title: '', description: '', status: 'todo', priority: 'medium', dueDate: '', assignee: '' })
    setTaskEditorOpen(true)
  }, [projects])
  const saveTask = async (e: FormEvent) => {
    e.preventDefault()
    if (!taskDraft.title.trim()) return
    try {
      await apiFetch('/api/tasks', { method: editingTask ? 'PUT' : 'POST', body: JSON.stringify({ ...taskDraft, id: editingTask?.id }) })
      setTaskEditorOpen(false); await refresh()
    } catch (err) { setError((err as Error).message) }
  }
  const deleteTask = async (id: string) => {
    if (!confirm('Delete this task?')) return
    try { await apiFetch('/api/tasks', { method: 'DELETE', body: JSON.stringify({ id }) }); await refresh() } catch (e) { setError((e as Error).message) }
  }
  const moveTask = async (taskId: string, status: TaskStatus) => {
    const task = tasks.find((t) => t.id === taskId)
    if (!task) return
    try { await apiFetch('/api/tasks', { method: 'PUT', body: JSON.stringify({ ...task, status }) }); await refresh() } catch (e) { setError((e as Error).message) }
  }

  const openPageEditor = async (page?: Page) => {
    if (page) {
      let full = page
      if (page.content === undefined || page.content === '') {
        try { full = await apiFetch<Page>(`/api/pages?id=${page.id}`) } catch {}
      }
      setPageEditor(full); setPageDraft({ title: full.title, icon: full.icon ?? '', content: full.content })
    }
    else {
      try {
        const created = await apiFetch<Page>('/api/pages', { method: 'POST', body: JSON.stringify({ title: 'Untitled', workspace: true, createdBy: 'You' }) })
        setPageEditor(created); setPageDraft({ title: created.title, icon: created.icon ?? '', content: created.content })
        await refresh()
      } catch (e) { setError((e as Error).message) }
    }
    setView('page')
  }
  const deletePage = async (id: string) => {
    if (!confirm('Delete this page?')) return
    try { await apiFetch('/api/pages', { method: 'DELETE', body: JSON.stringify({ id }) }); setView('home'); await refresh() } catch (e) { setError((e as Error).message) }
  }

  const createCycle = async () => {
    const name = window.prompt('Cycle name')
    if (!name) return
    try { await apiFetch('/api/cycles', { method: 'POST', body: JSON.stringify({ projectId: selectedProject === 'all' ? projects[0]?.id : selectedProject, name }) }); await refresh() } catch (e) { setError((e as Error).message) }
  }
  const createModule = async () => {
    const name = window.prompt('Module name')
    if (!name) return
    try { await apiFetch('/api/modules', { method: 'POST', body: JSON.stringify({ projectId: selectedProject === 'all' ? projects[0]?.id : selectedProject, name }) }); await refresh() } catch (e) { setError((e as Error).message) }
  }
  const createSticky = async () => {
    const title = window.prompt('Sticky title')
    if (!title) return
    const body = window.prompt('Sticky body') || ''
    try { await apiFetch('/api/stickies', { method: 'POST', body: JSON.stringify({ title, body, color: 'yellow' }) }); await refresh() } catch (e) { setError((e as Error).message) }
  }
  const createDraft = async () => {
    const title = window.prompt('Draft title')
    if (!title) return
    try { await apiFetch('/api/drafts', { method: 'POST', body: JSON.stringify({ kind: 'note', title, content: '' }) }); await refresh() } catch (e) { setError((e as Error).message) }
  }
  const createIntake = async () => {
    const title = window.prompt('Intake title')
    if (!title) return
    const pid = selectedProject === 'all' ? projects[0]?.id : selectedProject
    if (!pid) return
    try { await apiFetch('/api/intake', { method: 'POST', body: JSON.stringify({ projectId: pid, title }) }); await refresh() } catch (e) { setError((e as Error).message) }
  }
  const markAllRead = async () => {
    try { await apiFetch('/api/notifications', { method: 'PUT', body: JSON.stringify({ markAllRead: true }) }); await refresh() } catch (e) { setError((e as Error).message) }
  }
  const doSearch = async (q: string) => {
    setSearch(q)
    if (!q.trim()) { setSearchResults(null); return }
    try { const r = await apiFetch<any>(`/api/search?q=${encodeURIComponent(q)}`); setSearchResults(r) } catch (e) { setError((e as Error).message) }
  }

  // AI
  const sendChat = async () => {
    if (!prompt.trim() || !selectedProvider) return
    const userMsg: ChatMessage = { role: 'user', content: prompt, id: uid() }
    setMessages((m) => [...m, userMsg])
    setPrompt('')
    try {
      const res = await apiFetch<ChatResponse>('/api/ai/chat', { method: 'POST', body: JSON.stringify({ message: userMsg.content, sessionId: sessionId || undefined, providerId: selectedProvider, streaming: false }) })
      setSessionId(res.sessionId)
      setMessages((m) => [...m, { role: 'assistant', content: res.response, id: uid() }])
      if (res.pendingApprovals) setApprovals(res.pendingApprovals)
      await refresh()
    } catch (e) { setError((e as Error).message) }
  }

  const [iconTarget, setIconTarget] = useState<Page | null>(null)
  const [iconDraft, setIconDraft] = useState('')
  const [cmdOpen, setCmdOpen] = useState(false)
  const unreadCount = notifications.filter((n) => !n.read).length
  const [cmdQuery, setCmdQuery] = useState('')
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setCmdOpen(true) }
      if (e.key === 'Escape') setCmdOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  const cmdResults = useMemo(() => {
    const q = cmdQuery.toLowerCase()
    const items: { label: string; action: () => void }[] = []
    pages.forEach((p) => { if (!q || p.title.toLowerCase().includes(q)) items.push({ label: `📄 ${p.title}`, action: () => { setSelectedPage(p.id); openPageEditor(p); setCmdOpen(false) } }) })
    projects.forEach((p) => { if (!q || p.name.toLowerCase().includes(q)) items.push({ label: `📁 ${p.name}`, action: () => { setView('projects'); setCmdOpen(false) } }) })
    tasks.forEach((t) => { if (!q || t.title.toLowerCase().includes(q)) items.push({ label: `✅ ${t.title}`, action: () => { openTaskEditor(t); setCmdOpen(false) } }) })
    return items.slice(0, 20)
  }, [cmdQuery, pages, projects, tasks, openTaskEditor])

  const sidebar = (
    <aside className={`sidebar ${sidebarCollapsed ? 'collapsed' : ''}`}>
      <div className="workspace-name">🚀 {workspaces[0]?.name || 'Reverie'}</div>
      <div style={{ padding: '4px 8px' }}>
        <input className="search-box" placeholder="Search..." value={search} onChange={(e) => doSearch(e.target.value)} onFocus={() => setView('search')} />
      </div>
      <div className="nav-section">Workspace</div>
      <div className={`nav-item ${view === 'home' ? 'active' : ''}`} onClick={() => setView('home')}>🏠 Home</div>
      <div className={`nav-item ${view === 'page' && !pageEditor ? 'active' : ''}`} onClick={() => openPageEditor()}>📄 New page</div>
      <div className={`nav-item ${view === 'notifications' ? 'active' : ''}`} onClick={() => setView('notifications')}>🔔 Notifications {unreadCount > 0 && <span className="notif-dot" />}</div>
      <div className="nav-section">Planning</div>
      <div className={`nav-item ${view === 'issues' ? 'active' : ''}`} onClick={() => setView('issues')}>✅ Issues</div>
      <div className={`nav-item ${view === 'projects' ? 'active' : ''}`} onClick={() => setView('projects')}>📁 Projects</div>
      <div className={`nav-item ${view === 'cycles' ? 'active' : ''}`} onClick={() => setView('cycles')}>🔄 Cycles</div>
      <div className={`nav-item ${view === 'modules' ? 'active' : ''}`} onClick={() => setView('modules')}>🧩 Modules</div>
      <div className={`nav-item ${view === 'intake' ? 'active' : ''}`} onClick={() => setView('intake')}>📥 Intake</div>
      <div className="nav-section">Knowledge</div>
      <div className={`nav-item ${view === 'stickies' ? 'active' : ''}`} onClick={() => setView('stickies')}>📌 Stickies</div>
      <div className={`nav-item ${view === 'drafts' ? 'active' : ''}`} onClick={() => setView('drafts')}>📝 Drafts</div>
      <div className="nav-section">Pages</div>
      {(() => {
        const roots = pages.filter((p) => p.workspace && !p.parentId)
        const childrenOf = (pid: string) => pages.filter((p) => p.parentId === pid)
        const renderNode = (p: Page, depth: number): any => (
          <div key={p.id}>
            <div className={`tree-item ${selectedPage === p.id ? 'active' : ''}`} style={{ paddingLeft: 8 + depth * 12 }} onClick={() => { setSelectedPage(p.id); openPageEditor(p) }}>
              <span>{childrenOf(p.id).length > 0 ? '▾' : '📄'}</span>
              <span style={{ marginRight: 4 }}>{p.icon || '📄'}</span>
              <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{p.title}</span>
            </div>
            {childrenOf(p.id).map((c) => renderNode(c, depth + 1))}
          </div>
        )
        return roots.map((p) => renderNode(p, 0))
      })()}
      <div className="nav-section">AI</div>
      <div className={`nav-item ${view === 'assistant' ? 'active' : ''}`} onClick={() => setView('assistant')}>🤖 Assistant</div>
      <div className={`nav-item ${view === 'settings' ? 'active' : ''}`} onClick={() => setView('settings')}>⚙️ Settings</div>
      <div className="nav-section">Analytics</div>
      <div className={`nav-item ${view === 'analytics' ? 'active' : ''}`} onClick={() => setView('analytics')}>📊 Analytics</div>
    </aside>
  )

  const renderContent = () => {
    if (view === 'home') return (
      <div className="page-wrap">
        <h1 style={{ fontSize: 32 }}>Welcome back</h1>
        <p className="empty">Overview of your workspace</p>
        {overview && (
          <div className="row" style={{ gap: 16, marginTop: 24, flexWrap: 'wrap' }}>
            <div className="card"><div style={{ fontSize: 24, fontWeight: 700 }}>{overview.projectCount}</div><div className="empty">Projects</div></div>
            <div className="card"><div style={{ fontSize: 24, fontWeight: 700 }}>{overview.taskCount}</div><div className="empty">Issues</div></div>
            <div className="card"><div style={{ fontSize: 24, fontWeight: 700 }}>{overview.completedCount}</div><div className="empty">Completed</div></div>
            <div className="card"><div style={{ fontSize: 24, fontWeight: 700 }}>{overview.upcomingCount}</div><div className="empty">Upcoming</div></div>
          </div>
        )}
        <h2 style={{ marginTop: 32 }}>Recent pages</h2>
        {pages.slice(0, 6).map((p) => (
          <div key={p.id} className="tree-item" onClick={() => { setSelectedPage(p.id); openPageEditor(p) }} style={{ padding: '8px 0' }}>
            <span>{p.icon || '📄'}</span><span>{p.title}</span>
          </div>
        ))}
        <h2 style={{ marginTop: 32 }}>Stickies</h2>
        <div className="row" style={{ flexWrap: 'wrap', gap: 12 }}>
          {stickies.map((s) => <div key={s.id} className="card" style={{ width: 200, background: s.color === 'yellow' ? '#fff9c4' : 'var(--card)' }}><b>{s.title}</b><div style={{ fontSize: 13 }}>{s.body}</div></div>)}
          <button className="btn" onClick={createSticky}>+ Sticky</button>
        </div>
      </div>
    )

  if (view === 'page') return (
      <div className="page-wrap">
        <div className="row" style={{ justifyContent: 'space-between', marginBottom: 16 }}>
          <button className="btn" onClick={() => setView('home')}>← Back</button>
          <div className="row">
            {pageEditor && <button className="btn" onClick={() => deletePage(pageEditor.id)}>Delete</button>}
          </div>
        </div>
        <div className="page-icon" onClick={() => { if (!pageEditor) return; setIconDraft(pageEditor.icon || ''); setIconTarget(pageEditor) }}>{pageDraft.icon || '📄'}</div>
        <input className="page-title" value={pageDraft.title} onChange={(e) => { setPageDraft((d) => ({ ...d, title: e.target.value })); pageTitleRef.current = e.target.value }} onBlur={() => { if (pageEditor) savePageContent(pageDraft.content || pageEditor.content).catch(() => {}) }} placeholder="Untitled" />
  {pageEditor && (
    <PageEditor
      key={pageEditor.id}
      initialContent={pageEditor.content}
      theme={theme}
      onSave={savePageContent}
      onStatus={() => {}}
    />
  )}
      </div>
    )

    if (view === 'issues') return (
      <div style={{ padding: 16 }}>
        <div className="row" style={{ justifyContent: 'space-between', marginBottom: 12 }}>
          <h2>Issues</h2>
          <div className="row">
            <select className="input" value={selectedProject} onChange={(e) => setSelectedProject(e.target.value)}>
              <option value="all">All projects</option>
              {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
            <button className="btn primary" onClick={() => openTaskEditor()}>+ New issue</button>
          </div>
        </div>
        <div className="kanban">
          {(['todo', 'in_progress', 'done', 'blocked'] as TaskStatus[]).map((status) => (
            <div key={status} className="column" onDragOver={(e) => { e.preventDefault(); setDropTarget(status) }} onDrop={(e) => { e.preventDefault(); const id = draggedTaskId.current; if (id) moveTask(id, status); setDropTarget('') }} style={dropTarget === status ? { outline: '2px solid var(--accent)' } : {}}>
              <div className="column-title">{status.replace('_', ' ')} ({visibleTasks.filter((t) => t.status === status).length})</div>
              {visibleTasks.filter((t) => t.status === status).map((t) => (
                <div key={t.id} className="task-card" draggable onDragStart={() => { draggedTaskId.current = t.id }} onClick={() => openTaskEditor(t)}>
                  <div>{t.title}</div>
                  <div style={{ fontSize: 11, color: 'var(--muted)', marginTop: 4 }}>{t.priority} {t.dueDate ? `· ${t.dueDate}` : ''}</div>
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    )

    if (view === 'projects') return (
      <div style={{ padding: 24 }}>
        <div className="row" style={{ justifyContent: 'space-between', marginBottom: 16 }}>
          <h2>Projects</h2>
          <button className="btn primary" onClick={() => openProjectEditor()}>+ New project</button>
        </div>
        <table className="table">
          <thead><tr><th>Name</th><th>Status</th><th>Owner</th><th></th></tr></thead>
          <tbody>
            {projects.map((p) => (
              <tr key={p.id}>
                <td><span style={{ color: p.color === 'cyan' ? 'var(--accent)' : 'inherit' }}>●</span> {p.name}<div style={{ fontSize: 12, color: 'var(--muted)' }}>{p.description}</div></td>
                <td>{p.status}</td>
                <td>{p.owner}</td>
                <td className="row"><button className="btn" onClick={() => openProjectEditor(p)}>Edit</button><button className="btn" onClick={() => deleteProject(p.id)}>Archive</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )

    if (view === 'cycles') return (
      <div style={{ padding: 24 }}>
        <div className="row" style={{ justifyContent: 'space-between', marginBottom: 16 }}><h2>Cycles</h2><button className="btn primary" onClick={createCycle}>+ New cycle</button></div>
        <table className="table"><thead><tr><th>Name</th><th>Project</th><th>Dates</th><th>Status</th></tr></thead><tbody>
          {cycles.map((c) => <tr key={c.id}><td>{c.name}</td><td>{projects.find((p) => p.id === c.projectId)?.name}</td><td>{c.startDate} → {c.endDate}</td><td>{c.status}</td></tr>)}
        </tbody></table>
      </div>
    )
    if (view === 'modules') return (
      <div style={{ padding: 24 }}>
        <div className="row" style={{ justifyContent: 'space-between', marginBottom: 16 }}><h2>Modules</h2><button className="btn primary" onClick={createModule}>+ New module</button></div>
        <table className="table"><thead><tr><th>Name</th><th>Project</th><th>Status</th><th>Lead</th></tr></thead><tbody>
          {modules.map((m) => <tr key={m.id}><td>{m.name}<div style={{ fontSize: 12, color: 'var(--muted)' }}>{m.description}</div></td><td>{projects.find((p) => p.id === m.projectId)?.name}</td><td>{m.status}</td><td>{m.lead}</td></tr>)}
        </tbody></table>
      </div>
    )
    if (view === 'intake') return (
      <div style={{ padding: 24 }}>
        <div className="row" style={{ justifyContent: 'space-between', marginBottom: 16 }}><h2>Intake</h2><button className="btn primary" onClick={createIntake}>+ New intake</button></div>
        <table className="table"><thead><tr><th>Title</th><th>Project</th><th>Status</th></tr></thead><tbody>
          {intake.map((i) => <tr key={i.id}><td>{i.title}<div style={{ fontSize: 12, color: 'var(--muted)' }}>{i.body}</div></td><td>{projects.find((p) => p.id === i.projectId)?.name}</td><td>{i.status}</td></tr>)}
        </tbody></table>
      </div>
    )
    if (view === 'stickies') return (
      <div style={{ padding: 24 }}>
        <div className="row" style={{ justifyContent: 'space-between', marginBottom: 16 }}><h2>Stickies</h2><button className="btn primary" onClick={createSticky}>+ New sticky</button></div>
        <div className="row" style={{ flexWrap: 'wrap', gap: 12 }}>
          {stickies.map((s) => <div key={s.id} className="card" style={{ width: 220 }}><b>{s.title}</b><div style={{ fontSize: 13, marginTop: 4 }}>{s.body}</div></div>)}
        </div>
      </div>
    )
    if (view === 'drafts') return (
      <div style={{ padding: 24 }}>
        <div className="row" style={{ justifyContent: 'space-between', marginBottom: 16 }}><h2>Drafts</h2><button className="btn primary" onClick={createDraft}>+ New draft</button></div>
        <table className="table"><thead><tr><th>Title</th><th>Kind</th><th>Updated</th></tr></thead><tbody>
          {drafts.map((d) => <tr key={d.id}><td>{d.title}</td><td>{d.kind}</td><td>{d.updatedAt}</td></tr>)}
        </tbody></table>
      </div>
    )
    if (view === 'notifications') return (
      <div style={{ padding: 24 }}>
        <div className="row" style={{ justifyContent: 'space-between', marginBottom: 16 }}><h2>Notifications</h2><button className="btn" onClick={markAllRead}>Mark all read</button></div>
        {notifications.map((n) => (
          <div key={n.id} className="card" style={{ marginBottom: 8, opacity: n.read ? 0.6 : 1 }}>
            <b>{n.title}</b> {n.read ? '' : <span className="notif-dot" />}
            <div style={{ fontSize: 13, color: 'var(--muted)' }}>{n.body}</div>
          </div>
        ))}
      </div>
    )
    if (view === 'analytics') return (
      <div style={{ padding: 24 }}>
        <h2>Analytics</h2>
        {analytics && (
          <div className="row" style={{ gap: 16, flexWrap: 'wrap', marginTop: 16 }}>
            <div className="card"><div style={{ fontSize: 24, fontWeight: 700 }}>{analytics.totalProjects}</div><div className="empty">Projects</div></div>
            <div className="card"><div style={{ fontSize: 24, fontWeight: 700 }}>{analytics.totalTasks}</div><div className="empty">Issues</div></div>
            <div className="card"><div style={{ fontSize: 24, fontWeight: 700 }}>{analytics.totalPages}</div><div className="empty">Pages</div></div>
            <div className="card"><div style={{ fontSize: 24, fontWeight: 700 }}>{analytics.totalCycles}</div><div className="empty">Cycles</div></div>
            <div className="card"><div style={{ fontSize: 24, fontWeight: 700 }}>{analytics.totalModules}</div><div className="empty">Modules</div></div>
          </div>
        )}
        {analytics?.taskStatusCounts && (
          <div style={{ marginTop: 24 }}>
            <h3>Issue status</h3>
            <table className="table"><tbody>
              {Object.entries(analytics.taskStatusCounts).map(([k, v]) => <tr key={k}><td>{k}</td><td>{v as number}</td></tr>)}
            </tbody></table>
          </div>
        )}
      </div>
    )
    if (view === 'search') return (
      <div style={{ padding: 24 }}>
        <h2>Search results for "{search}"</h2>
        {searchResults && (
          <>
            <h3>Projects</h3>
            {searchResults.projects?.map((p: Project) => <div key={p.id} className="tree-item" onClick={() => setView('projects')}>{p.name}</div>)}
            <h3>Issues</h3>
            {searchResults.tasks?.map((t: Task) => <div key={t.id} className="tree-item" onClick={() => { openTaskEditor(t); }}>{t.title}</div>)}
            <h3>Pages</h3>
            {searchResults.pages?.map((p: Page) => <div key={p.id} className="tree-item" onClick={() => { setSelectedPage(p.id); openPageEditor(p) }}>{p.title}</div>)}
          </>
        )}
      </div>
    )
    if (view === 'settings') return (
      <div style={{ padding: 24, maxWidth: 720 }}>
        <h2>Settings</h2>
        <div className="form-row"><label>Theme</label>
          <select className="input" value={theme} onChange={(e) => setTheme(e.target.value as any)}>
            <option value="light">Light</option><option value="dark">Dark</option>
          </select>
        </div>
        <h3 style={{ marginTop: 24 }}>AI Providers</h3>
        <div className="row" style={{ marginBottom: 12 }}>
          <select className="input" value={selectedProvider} onChange={(e) => setSelectedProvider(e.target.value)}>
            {providers.map((p) => <option key={p.id} value={p.id}>{p.label}</option>)}
          </select>
        </div>
        <p className="empty">Provider configuration available via existing controls.</p>
        <h3 style={{ marginTop: 24 }}>Members</h3>
        {members.map((m: any) => <div key={m.id}>{m.name} — {m.role}</div>)}
        <h3 style={{ marginTop: 24 }}>Workspaces</h3>
        {workspaces.map((w: any) => <div key={w.id}>{w.name}</div>)}
      </div>
    )
    if (view === 'assistant') return (
      <div style={{ padding: 24, maxWidth: 800, margin: '0 auto' }}>
        <h2>Assistant</h2>
        <div className="row" style={{ marginBottom: 12 }}>
          <select className="input" value={selectedProvider} onChange={(e) => setSelectedProvider(e.target.value)}>
            {providers.map((p) => <option key={p.id} value={p.id}>{p.label}</option>)}
          </select>
        </div>
        <div style={{ border: '1px solid var(--border)', borderRadius: 8, padding: 12, minHeight: 300, maxHeight: 400, overflowY: 'auto', marginBottom: 12 }}>
          {messages.map((m) => <div key={m.id} style={{ marginBottom: 8 }}><b>{m.role}:</b> {m.content}</div>)}
        </div>
        <div className="row">
          <input className="input" style={{ flex: 1 }} value={prompt} onChange={(e) => setPrompt(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') sendChat() }} placeholder="Ask anything..." />
          <button className="btn primary" onClick={sendChat}>Send</button>
        </div>
        {approvals.length > 0 && <div style={{ marginTop: 12 }}><b>Pending approvals:</b> {approvals.length}</div>}
      </div>
    )
    return <div className="page-wrap"><p className="empty">Not found</p></div>
  }

  return (
    <div className="app" data-theme={theme}>
      <button className="btn" style={{ position: 'fixed', top: 8, left: 8, zIndex: 20 }} onClick={() => setSidebarCollapsed((c) => !c)}>☰</button>
      {sidebar}
      <main className="main">
        <div className="topbar">
          <span style={{ cursor: 'pointer' }} onClick={() => setView('home')}>Home</span>
          <span>/</span>
          <span>{view}</span>
          <span style={{ flex: 1 }} />
          <button className="btn" onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')}>{theme === 'light' ? '🌙' : '☀️'}</button>
        </div>
        <div className="content">{renderContent()}</div>
      </main>

      {taskEditorOpen && (
        <div className="modal-backdrop" onClick={() => setTaskEditorOpen(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>{editingTask ? 'Edit issue' : 'New issue'}</h3>
            <form onSubmit={saveTask}>
              <div className="form-row"><label>Title</label><input className="input" aria-label="Title" value={taskDraft.title} onChange={(e) => setTaskDraft({ ...taskDraft, title: e.target.value })} required /></div>
              <div className="form-row"><label>Description</label><textarea className="input" aria-label="Description" value={taskDraft.description} onChange={(e) => setTaskDraft({ ...taskDraft, description: e.target.value })} /></div>
              <div className="form-row"><label>Project</label>
                <select className="input" aria-label="Project" value={taskDraft.projectId} onChange={(e) => setTaskDraft({ ...taskDraft, projectId: e.target.value })}>
                  {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              </div>
              <div className="row">
                <div className="form-row" style={{ flex: 1 }}><label>Status</label>
                  <select className="input" aria-label="Status" value={taskDraft.status} onChange={(e) => setTaskDraft({ ...taskDraft, status: e.target.value as TaskStatus })}>
                    <option value="todo">Todo</option><option value="in_progress">In progress</option><option value="done">Done</option><option value="blocked">Blocked</option>
                  </select>
                </div>
                <div className="form-row" style={{ flex: 1 }}><label>Priority</label>
                  <select className="input" aria-label="Priority" value={taskDraft.priority} onChange={(e) => setTaskDraft({ ...taskDraft, priority: e.target.value as Priority })}>
                    <option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option>
                  </select>
                </div>
              </div>
              <div className="row">
                <div className="form-row" style={{ flex: 1 }}><label>Due date</label><input className="input" type="date" aria-label="Due date" value={taskDraft.dueDate} onChange={(e) => setTaskDraft({ ...taskDraft, dueDate: e.target.value })} /></div>
                <div className="form-row" style={{ flex: 1 }}><label>Assignee</label><input className="input" aria-label="Assignee" value={taskDraft.assignee} onChange={(e) => setTaskDraft({ ...taskDraft, assignee: e.target.value })} /></div>
              </div>
              <div className="row" style={{ justifyContent: 'flex-end' }}>
                {editingTask && <button type="button" className="btn" onClick={() => { deleteTask(editingTask.id); setTaskEditorOpen(false) }}>Delete</button>}
                <button type="button" className="btn" onClick={() => setTaskEditorOpen(false)}>Cancel</button>
                <button type="submit" className="btn primary">Save</button>
              </div>
            </form>
          </div>
        </div>
      )}

      {projectEditorOpen && (
        <div className="modal-backdrop" onClick={() => setProjectEditorOpen(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>{editingProject ? 'Edit project' : 'New project'}</h3>
            <form onSubmit={saveProject}>
              <div className="form-row"><label>Name</label><input className="input" aria-label="Name" value={projectDraft.name} onChange={(e) => setProjectDraft({ ...projectDraft, name: e.target.value })} required /></div>
              <div className="form-row"><label>Description</label><textarea className="input" aria-label="Description" value={projectDraft.description} onChange={(e) => setProjectDraft({ ...projectDraft, description: e.target.value })} /></div>
              <div className="row">
                <div className="form-row" style={{ flex: 1 }}><label>Status</label>
                  <select className="input" aria-label="Status" value={projectDraft.status} onChange={(e) => setProjectDraft({ ...projectDraft, status: e.target.value as ProjectStatus })}>
                    <option value="planning">Planning</option><option value="active">Active</option><option value="paused">Paused</option><option value="archived">Archived</option>
                  </select>
                </div>
                <div className="form-row" style={{ flex: 1 }}><label>Owner</label><input className="input" aria-label="Owner" value={projectDraft.owner} onChange={(e) => setProjectDraft({ ...projectDraft, owner: e.target.value })} /></div>
              </div>
              <div className="row" style={{ justifyContent: 'flex-end' }}>
                <button type="button" className="btn" onClick={() => setProjectEditorOpen(false)}>Cancel</button>
                <button type="submit" className="btn primary">Save</button>
              </div>
            </form>
          </div>
        </div>
      )}

      {iconTarget && (
        <div className="modal-backdrop" onClick={() => setIconTarget(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()} style={{ width: 320 }}>
            <h3>Page icon</h3>
            <input className="input" style={{ width: '100%', fontSize: 32, textAlign: 'center' }} value={iconDraft} onChange={(e) => setIconDraft(e.target.value)} autoFocus />
            <div className="row" style={{ justifyContent: 'flex-end', marginTop: 12 }}>
              <button className="btn" onClick={() => setIconTarget(null)}>Cancel</button>
              <button className="btn primary" onClick={async () => { if (!iconTarget) return; const ic = iconDraft; setPageDraft((d) => ({ ...d, icon: ic })); try { await apiFetch('/api/pages', { method: 'PUT', body: JSON.stringify({ ...iconTarget, icon: ic }) }); await refresh(); setIconTarget(null) } catch (e) { setError((e as Error).message) } }}>Save</button>
            </div>
          </div>
        </div>
      )}
      {cmdOpen && (
        <div className="modal-backdrop" onClick={() => setCmdOpen(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()} style={{ width: 480 }}>
            <input className="input" style={{ width: '100%', marginBottom: 8 }} autoFocus placeholder="Search pages, projects, issues..." value={cmdQuery} onChange={(e) => setCmdQuery(e.target.value)} />
            <div style={{ maxHeight: 320, overflowY: 'auto' }}>
              {cmdResults.map((r, i) => <div key={i} className="tree-item" onClick={r.action}>{r.label}</div>)}
              {cmdResults.length === 0 && <div className="empty">No results</div>}
            </div>
          </div>
        </div>
      )}

      {error && <div style={{ position: 'fixed', bottom: 16, right: 16, background: '#eb5757', color: 'white', padding: '8px 12px', borderRadius: 8 }}>{error}<button style={{ marginLeft: 8, background: 'transparent', border: 'none', color: 'white', cursor: 'pointer' }} onClick={() => setError('')}>×</button></div>}

    </div>
  )
}

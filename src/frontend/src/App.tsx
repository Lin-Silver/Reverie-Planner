import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import DatabaseView from './DatabaseView'
import WorkspaceStorage from './WorkspaceStorage'
import Icon from './Icon'
import Dialog from './Dialog'
import PageRecovery from './PageRecovery'
import { pageTemplates } from './pageTemplates'
import type { FormEvent } from 'react'
import './App.css'

const PageEditor = lazy(() => import('./PageEditor'))

type ProjectStatus = 'active' | 'planning' | 'paused' | 'archived'
type TaskStatus = 'todo' | 'in_progress' | 'done' | 'blocked'
type Priority = 'low' | 'medium' | 'high'
type Project = { id: string; name: string; description: string; status: ProjectStatus; owner: string; color: string; createdAt: string }
type Task = { id: string; projectId: string; title: string; description?: string; status: TaskStatus; priority: Priority; dueDate?: string; assignee?: string; createdAt: string; stateId?: string; cycleId?: string; moduleId?: string; estimate?: string; parentId?: string; labelsJson?: string }
type Overview = { projectCount: number; taskCount: number; completedCount: number; upcomingCount: number; aiEnabled: boolean }
type Provider = { id: string; label: string; provider: string; baseURL: string; model: string; temperature: number; maxTokens: number; enabled: boolean; streaming: boolean; toolCalling: boolean; apiKeyConfigured: boolean }
type TaskDraft = { projectId: string; title: string; description: string; status: TaskStatus; priority: Priority; dueDate: string; assignee: string }
type ChatMessage = { role: 'user' | 'assistant'; content: string; id: string }
type Page = { id: string; projectId?: string; workspace: boolean; parentId?: string; linkedDatabaseId?: string; linkedViewId?: string; title: string; icon?: string; cover?: string; content: string; version?: number; createdBy: string; createdAt: string; updatedAt: string }
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
  const [narrow, setNarrow] = useState(() => window.matchMedia('(max-width: 768px)').matches)
  useEffect(() => { const media = window.matchMedia('(max-width: 768px)'); const update = () => setNarrow(media.matches); media.addEventListener('change', update); return () => media.removeEventListener('change', update) }, [])
  const [library, setLibrary] = useState<{favorites: string[]; recent: string[]}>({favorites: [], recent: []})
  const [creatingTemplate, setCreatingTemplate] = useState(false)
  const [fullWidth, setFullWidth] = useState(() => localStorage.getItem('reverie.fullWidth') === '1')
  const [theme, setTheme] = useState<'light' | 'dark'>(() => (localStorage.getItem('theme') as any) || 'light')
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => localStorage.getItem('reverie.sidebar.collapsed') === '1' || (localStorage.getItem('reverie.sidebar.collapsed') === null && window.matchMedia('(max-width: 768px)').matches))
  const [view, _setView] = useState<string>('home')
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
  const [pageDatabaseId, setPageDatabaseId] = useState<string>('')
  const [pageDatabaseViewId, setPageDatabaseViewId] = useState<string>('')
  const databasePendingEditRef = useRef(false)
  const [pageDraft, setPageDraft] = useState({ title: 'Untitled', icon: '', content: '' })
  const [pageLeaveError, setPageLeaveError] = useState(false)
  // Keep the callback stable: PageEditor must not re-lock an in-flight Retry
  // merely because the parent rerenders with the conflict banner visible.
  const handlePageSaveStatus = useCallback((status: string) => {
    if (status === 'saved') setPageLeaveError(false)
    else if (status === 'conflict') setPageLeaveError(true)
  }, [])
  const [selectedProvider, setSelectedProvider] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [prompt, setPrompt] = useState('')
  const [approvals, setApprovals] = useState<Approval[]>([])
  const [dropTarget, setDropTarget] = useState<TaskStatus | ''>('')
  const draggedTaskId = useRef('')
  const pageEditorRef = useRef<Page | null>(null)
  const pageTitleRef = useRef('')
  // A local title draft must survive a stale-version response until the user resolves it.
  const pageTitleDirtyRef = useRef(false)
  useEffect(() => { pageEditorRef.current = pageEditor; pageTitleRef.current = pageDraft.title }, [pageEditor, pageDraft.title])
  const pageVersions = useRef<Map<string, number>>(new Map())
  const pageSnapshots = useRef<Map<string, Page>>(new Map())
  const pageDiscardRef = useRef(false)
  const pageWriteQueues = useRef<Map<string, Promise<void>>>(new Map())
  const savePageContent = async (pageId: string, content: string) => {
    // serialize per page
    const prev = pageWriteQueues.current.get(pageId) || Promise.resolve()
    const next = prev.then(async () => {
      // authoritative version and metadata from server snapshot (never stale pages[] cache)
      let snapshot = pageSnapshots.current.get(pageId)
      if (!snapshot) {
        const pe = pages.find((p) => p.id === pageId) || pageEditorRef.current
        if (!pe) return
        snapshot = pe
        pageSnapshots.current.set(pageId, snapshot)
      }
      let version = pageVersions.current.get(pageId)
      if (version == null) {
        version = snapshot.version || 1
      }
      // title comes from the live title input (user may be editing it), all other metadata
      // (icon, cover, parentId, project_id, workspace) comes from the server snapshot so a
      // Retry + continuous autosave never resurrects stale remote metadata.
      const title = pageEditorRef.current?.id === pageId ? pageTitleRef.current : snapshot.title
      try {
        const res = await apiFetch<Page>('/api/pages', { method: 'PUT', body: JSON.stringify({ ...snapshot, title, icon: snapshot.icon, cover: snapshot.cover, parentId: snapshot.parentId, content, version }) })
        if (res.version) {
          pageVersions.current.set(pageId, res.version)
          pageSnapshots.current.set(pageId, res)
          setPages(current => current.map(page => page.id === res.id ? res : page))
          if (pageEditorRef.current?.id === pageId) {
            if (pageTitleRef.current === res.title) pageTitleDirtyRef.current = false
            pageEditorRef.current.version = res.version
            pageEditorRef.current.title = res.title
            pageEditorRef.current.icon = res.icon
            pageEditorRef.current.cover = res.cover
            pageEditorRef.current.parentId = res.parentId
            pageEditorRef.current.content = res.content
          }
        }
      } catch (e) {
        if ((e as Error).message.includes('version conflict') && pageId) {
          try {
            const latest = await apiFetch<Page>(`/api/pages?id=${pageId}`)
            pageVersions.current.set(pageId, latest.version || 1)
            pageSnapshots.current.set(pageId, latest)
            if (pageEditorRef.current?.id === pageId) {
              pageEditorRef.current.version = latest.version
              pageEditorRef.current.title = latest.title
              pageEditorRef.current.icon = latest.icon
              pageEditorRef.current.cover = latest.cover
              pageEditorRef.current.parentId = latest.parentId
              pageEditorRef.current.content = latest.content
              if (!pageTitleDirtyRef.current) {
                pageTitleRef.current = latest.title
                setPageDraft(d => ({ ...d, title: latest.title }))
              }
            }
            // keep local content; throw conflict for UI
          } catch {}
        }
        throw e
      }
    })
    pageWriteQueues.current.set(pageId, next.catch(() => {}))
    return next
  }

  const setView = (next: string) => {
    if (view === 'page' && next !== 'page' && pageLeaveError) { setError('Resolve the page conflict with Retry or Reload latest before leaving.'); return }
    if (view === 'page' && next !== 'page' && databasePendingEditRef.current) { setError('Resolve the unsaved database edit with Retry or Reload latest before leaving.'); return }
    if (view === 'page' && next !== 'page' && pageEditor) {
      void (async () => {
        try {
          await savePageContent(pageEditor.id, pageDraft.content)
          setPageLeaveError(false)
          _setView(next)
        } catch (e) {
          setPageEditor((pe) => (pe ? { ...pe, content: pageDraft.content } : pe))
          setPageLeaveError(true)
          setError(`Save failed: ${(e as Error).message}`)
        }
      })()
      return
    }
    setPageLeaveError(false)
    _setView(next)
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
  const refreshLibrary = async () => setLibrary(await apiFetch('/api/page-library'))
  useEffect(() => { void refresh(); void refreshLibrary().catch(e => setError((e as Error).message)) }, [])
  useEffect(() => { localStorage.setItem('reverie.fullWidth', fullWidth ? '1' : '0') }, [fullWidth])
  const toggleFavorite = async (id: string) => {
    try {
      await apiFetch('/api/page-library', {method: 'POST', body: JSON.stringify({id, favorite: !library.favorites.includes(id)})})
      await refreshLibrary()
    } catch (e) { setError((e as Error).message) }
  }
  const createFromTemplate = async (id: string) => {
    const template = pageTemplates.find(t => t.id === id)
    if (!template || creatingTemplate) return
    setCreatingTemplate(true)
    try {
      const created = await apiFetch<Page>('/api/pages', {method: 'POST', body: JSON.stringify({title: template.title, content: JSON.stringify(template.blocks), workspace: true, createdBy: 'You'})})
      await refresh()
      await openPageEditor(created)
    } catch (e) { setError((e as Error).message) }
    finally { setCreatingTemplate(false) }
  }

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
      if (view === 'page' && pageLeaveError && pageEditor && (!page || page.id !== pageEditor.id)) { setError('Resolve the page conflict with Retry or Reload latest before opening another page.'); return }
      if (databasePendingEditRef.current && (!page || page.id !== pageEditor?.id)) { setError('Resolve the unsaved database edit before opening another page.'); return }
      if (view === 'page' && pageEditor && (!page || page.id !== pageEditor.id)) {
        try {
          await savePageContent(pageEditor.id, pageDraft.content)
        } catch (e) {
          setPageEditor((pe) => (pe ? { ...pe, content: pageDraft.content } : pe))
          setPageLeaveError(true)
          setError(`Save failed: ${(e as Error).message}`)
          return
        }
      }
      let openedId = page?.id
      let openedFull: Page | undefined = page
      if (page) {
        let full = page
        if (page.content === undefined || page.content === '' || page.linkedDatabaseId === undefined) {
          try { full = await apiFetch<Page>(`/api/pages?id=${page.id}`) } catch {}
        }
        pageVersions.current.set(full.id, full.version || 1)
        pageSnapshots.current.set(full.id, full)
        pageTitleDirtyRef.current = false
        setPageEditor(full); setPageDraft({ title: full.title, icon: full.icon ?? '', content: full.content })
        openedId = full.id
        openedFull = full
    }
      else {
        try {
          const created = await apiFetch<Page>('/api/pages', { method: 'POST', body: JSON.stringify({ title: 'Untitled', workspace: true, createdBy: 'You' }) })
          pageTitleDirtyRef.current = false
          setPageEditor(created); setPageDraft({ title: created.title, icon: created.icon ?? '', content: created.content })
          openedId = created.id
          pageVersions.current.set(created.id, created.version || 1)
          pageSnapshots.current.set(created.id, created)
          await refresh()
        } catch (e) { setError((e as Error).message); return }
      }
    // check if this page hosts a database
    try {
      const dbs = await apiFetch<any[]>('/api/databases')
      let found = (dbs || []).find((d: any) => d.pageId === openedId)
      if (!found && openedFull?.linkedDatabaseId) {
        found = (dbs || []).find((d: any) => d.id === openedFull!.linkedDatabaseId)
      }
      setPageDatabaseId(found ? found.id : '')
      setPageDatabaseViewId(openedFull?.linkedViewId || '')
    } catch {}
    if (openedId) {
      setSelectedPage(openedId)
      void apiFetch('/api/page-library', {method: 'POST', body: JSON.stringify({id: openedId, visit: true})}).then(refreshLibrary).catch(e => setError((e as Error).message))
    }
    if (window.matchMedia('(max-width: 768px)').matches) setSidebarCollapsed(true)
    setView('page')
  }
  const [recovery, setRecovery] = useState<{pageId?: string; version?: number; returnFocus?: HTMLElement | null} | null>(null)
  const [pageRecoveryBusy, setPageRecoveryBusy] = useState(false)
  const openHistory = async () => {
    if (!pageEditor || pageRecoveryBusy) return
    const returnFocus = document.activeElement as HTMLElement | null
    setPageRecoveryBusy(true)
    try {
      if (pageLeaveError) throw new Error('Resolve the page conflict before opening history.')
      await savePageContent(pageEditor.id, pageDraft.content)
      setRecovery({pageId: pageEditor.id, version: pageVersions.current.get(pageEditor.id), returnFocus})
    } catch (e) { setError((e as Error).message) }
    finally { setPageRecoveryBusy(false) }
  }
  const deletePage = async (id: string) => {
    if (pageLeaveError || pageRecoveryBusy) { setError('Resolve the page conflict before moving it to trash.'); return }
    if (!confirm('Move this page to trash? You can restore it from Page trash. Move child pages first.')) return
    setPageRecoveryBusy(true)
    try {
      await savePageContent(id, pageDraft.content)
      await apiFetch(`/api/pages?id=${encodeURIComponent(id)}&version=${pageVersions.current.get(id)}`, { method: 'DELETE' })
      pageDiscardRef.current = true
      setPageEditor(null); _setView('home'); await refresh()
    } catch (e) { setError((e as Error).message) }
    finally { setPageRecoveryBusy(false) }
  }
  const reloadRecoveredPage = async () => {
    if (!pageEditor) return
    const latest = await apiFetch<Page>(`/api/pages?id=${encodeURIComponent(pageEditor.id)}`)
    pageVersions.current.set(latest.id, latest.version || 1)
    pageSnapshots.current.set(latest.id, latest)
    pageTitleDirtyRef.current = false
    pageEditorRef.current = latest; pageTitleRef.current = latest.title
    setPageEditor(latest); setPageDraft({title: latest.title, icon: latest.icon || '', content: latest.content})
    setPageLeaveError(false); setReloadToken(t => t + 1)
    await refresh()
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

  const [expandedPages, setExpandedPages] = useState<Set<string>>(() => {
    try { return new Set(JSON.parse(localStorage.getItem('reverie.expanded') || '[]')) } catch { return new Set() }
  })
  useEffect(() => { localStorage.setItem('reverie.expanded', JSON.stringify([...expandedPages])) }, [expandedPages])
  const toggleExpanded = (id: string) => setExpandedPages((s) => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n })
  const [iconDraft, setIconDraft] = useState('')
  const [iconTarget, setIconTarget] = useState<Page | null>(null)
  const [reloadToken, setReloadToken] = useState(0)
  useEffect(() => { pageDiscardRef.current = false }, [reloadToken])
  const [cmdOpen, setCmdOpen] = useState(false)
  const unreadCount = notifications.filter((n) => !n.read).length
  const [cmdIndex, setCmdIndex] = useState(0)
  const [cmdQuery, setCmdQuery] = useState('')
  useEffect(() => { setCmdIndex(0) }, [cmdQuery])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setCmdOpen(true) }
      if (e.key === 'Escape') setCmdOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  const cmdResults = (() => {
    const q = cmdQuery.toLowerCase()
    const items: { label: string; action: () => void }[] = []
    pages.forEach((p) => { if (!q || p.title.toLowerCase().includes(q)) items.push({ label: `📄 ${p.title}`, action: () => { setSelectedPage(p.id); openPageEditor(p); setCmdOpen(false) } }) })
    projects.forEach((p) => { if (!q || p.name.toLowerCase().includes(q)) items.push({ label: `📁 ${p.name}`, action: () => { setView('projects'); setCmdOpen(false) } }) })
    tasks.forEach((t) => { if (!q || t.title.toLowerCase().includes(q)) items.push({ label: `✅ ${t.title}`, action: () => { openTaskEditor(t); setCmdOpen(false) } }) })
    if (q) {
      items.unshift({ label: `➕ Create page "${cmdQuery}"`, action: () => { apiFetch<Page>('/api/pages', { method: 'POST', body: JSON.stringify({ title: cmdQuery, workspace: true, createdBy: 'You' }) }).then((p) => { refresh(); setSelectedPage(p.id); openPageEditor(p); setCmdOpen(false) }).catch((e) => setError((e as Error).message)) } })
    }
    return items.slice(0, 20)
  })()

  const navigate = (next: string) => {
    setView(next)
    if (window.matchMedia('(max-width: 768px)').matches) setSidebarCollapsed(true)
  }
  const nav = (target: string, label: string, icon: string) => <button key={target} className={`nav-item ${view === target ? 'active' : ''}`} aria-current={view === target ? 'page' : undefined} onClick={() => navigate(target)}><Icon name={icon} /><span>{label}</span>{target === 'notifications' && unreadCount > 0 && <span className="nav-count">{unreadCount}</span>}</button>
  const favoritePages = library.favorites.map(id => pages.find(p => p.id === id)).filter((p): p is Page => !!p)
  const recentPages = library.recent.map(id => pages.find(p => p.id === id)).filter((p): p is Page => !!p)
  const pageCard = (p: Page) => <button key={p.id} className="page-card" onClick={() => void openPageEditor(p)}><span className="page-card-icon">{p.icon || <Icon name="page" size={24} />}</span><strong>{p.title}</strong><span className="page-card-meta">{p.parentId ? 'Nested page' : 'Workspace page'}<Icon name="arrow" size={16} /></span></button>
  const sidebar = (
    <aside id="workspace-sidebar" aria-label="Workspace navigation" className={`sidebar ${sidebarCollapsed ? 'collapsed' : ''}`} inert={sidebarCollapsed}>
      <div className="workspace-name"><span className="brand-mark"><Icon name="spark" size={22} /></span><div><strong>{workspaces[0]?.name || 'Reverie'}</strong><small>Personal workspace</small></div></div>
      <button className="command-trigger" onClick={() => setCmdOpen(true)}><Icon name="search" /><span>Quick find</span><kbd>⌘ / Ctrl K</kbd></button>
      <input className="search-box" aria-label="Search workspace" placeholder="Search workspace…" value={search} onChange={e => doSearch(e.target.value)} onFocus={() => setView('search')} />
      <div className="sidebar-scroll">
      <div className="nav-section">Workspace</div>
      {nav('home', 'Home', 'home')}
      <button className="nav-item" onClick={() => void openPageEditor()}><Icon name="plus" /><span>New page</span></button>
      {nav('templates', 'Templates', 'grid')}
      {favoritePages.length > 0 && <><div className="nav-section">Favorites</div>{favoritePages.map(p => <button key={p.id} className="nav-item favorite-link" onClick={() => void openPageEditor(p)}><Icon name="star" /><span>{p.title}</span></button>)}</>}
      <div className="nav-section">Planning</div>
      {nav('issues', 'Issues', 'check')}{nav('projects', 'Projects', 'folder')}
      <details className="nav-disclosure"><summary>More tools</summary>
        {nav('cycles', 'Cycles', 'clock')}{nav('modules', 'Modules', 'grid')}{nav('intake', 'Intake', 'folder')}
        {nav('stickies', 'Stickies', 'page')}{nav('drafts', 'Drafts', 'page')}{nav('analytics', 'Analytics', 'chart')}
      </details>
      <button className="nav-item" onClick={() => setRecovery({})}><Icon name="page" /><span>Page trash</span></button>
      <div className="nav-section page-section"><span>Pages</span><button className="icon-button" aria-label="Add page" onClick={() => void openPageEditor()}><Icon name="plus" size={16} /></button></div>
      {(() => {
        const roots = pages.filter((p) => p.workspace && !p.parentId)
        const childrenOf = (pid: string) => pages.filter((p) => p.parentId === pid)
        const isDescendant = (ancestor: string, target: string): boolean => {
          const kids = childrenOf(ancestor)
          if (kids.some((k) => k.id === target)) return true
          return kids.some((k) => isDescendant(k.id, target))
        }
        const renderNode = (p: Page, depth: number): any => {
          const kids = childrenOf(p.id)
          const expanded = expandedPages.has(p.id)
          return (
            <div key={p.id}>
              <div className={`tree-item ${selectedPage === p.id ? 'active' : ''}`} style={{ paddingLeft: 8 + depth * 12 }}
                draggable
                onDragStart={(e) => { e.dataTransfer.setData('text/plain', p.id) }}
                onDragOver={(e) => { e.preventDefault() }}
                onDrop={(e) => {
                  e.preventDefault()
                  const draggedId = e.dataTransfer.getData('text/plain')
                  if (!draggedId || draggedId === p.id) return
                  if (isDescendant(draggedId, p.id)) return
                  const dragged = pages.find((x) => x.id === draggedId)
                  apiFetch('/api/pages/move', { method: 'POST', body: JSON.stringify({ id: draggedId, parentId: p.id, version: dragged?.version }) }).then(() => refresh()).catch((err) => setError((err as Error).message))
                }}
                >
                {kids.length > 0 ? <button className="tree-toggle" aria-label={`${expanded ? 'Collapse' : 'Expand'} ${p.title}`} aria-expanded={expanded} onClick={() => toggleExpanded(p.id)}>{expanded ? '▾' : '▸'}</button> : <span className="tree-spacer" />}
                <button className="tree-open" onClick={() => void openPageEditor(p)}><span>{p.icon || <Icon name="page" size={16} />}</span><span>{p.title}</span></button>
                <button className="btn" aria-label={`Add child page to ${p.title}`} style={{ padding: '0 4px', fontSize: 11 }} onClick={(e) => { e.stopPropagation(); apiFetch('/api/pages', { method: 'POST', body: JSON.stringify({ title: 'Untitled', parentId: p.id, workspace: true, createdBy: 'You' }) }).then(() => refresh()).catch((err) => setError((err as Error).message)) }}>+</button>
              </div>
              {expanded && kids.map((c) => renderNode(c, depth + 1))}
            </div>
          )
        }
        return roots.map((p) => renderNode(p, 0))
      })()}
      </div>
      <div className="sidebar-footer">{nav('notifications', 'Notifications', 'bell')}{nav('assistant', 'Assistant', 'spark')}{nav('settings', 'Settings', 'settings')}<span className="local-status"><span />Stored on this device</span></div>
    </aside>
  )

  const renderContent = () => {
    if (view === 'templates') return (
      <div className="page-wrap dashboard">
        <div className="eyebrow">A little structure, a clear start</div><h1>Make space for your ideas.</h1>
        <p className="lead">Start with a template. Everything is editable and saved in your workspace.</p>
        <div className="template-grid">{pageTemplates.map(t => <button className="template-card" key={t.id} disabled={creatingTemplate} onClick={() => void createFromTemplate(t.id)}><span className="template-icon"><Icon name={t.icon} size={26} /></span><strong>{t.title}</strong><span>{t.description}</span><span className="template-action">{creatingTemplate ? 'Creating…' : 'Use template'}<Icon name="arrow" size={16} /></span></button>)}</div>
      </div>
    )
    if (view === 'home') return (
      <div className="page-wrap dashboard">
        <div className="home-heading"><div><div className="eyebrow">Your personal workspace</div><h1>Welcome back</h1><p className="lead">A calm place to think, plan and make progress.</p></div><button className="btn primary" onClick={() => void openPageEditor()}><Icon name="plus" size={16} />Create a page</button></div>
        <section className="quick-start"><div className="quick-start-copy"><span className="eyebrow">From a thought to a plan</span><h2>What’s on your mind?</h2><p>Capture an idea, plan a project, or start your next chapter.</p><button className="btn" onClick={() => navigate('templates')}>Explore templates<Icon name="arrow" size={16} /></button></div><div className="quick-start-art" aria-hidden="true"><span className="art-sheet"><Icon name="page" size={28} /><i /><i /><i /><span>Room for a new idea.</span></span></div></section>
        <section aria-label="Workspace overview" className="overview-strip">{[[pages.length, 'Pages'], [overview?.projectCount ?? 0, 'Projects'], [overview?.taskCount ?? 0, 'Issues'], [overview?.completedCount ?? 0, 'Completed']].map(([value, label]) => <div key={label}><strong>{value}</strong><span>{label}</span></div>)}</section>
        {favoritePages.length > 0 && <section><div className="section-heading"><h2><Icon name="star" />Favorites</h2><span>Pinned for easy access</span></div><div className="page-grid">{favoritePages.map(pageCard)}</div></section>}
        <section><div className="section-heading"><h2><Icon name="clock" />Recent pages</h2><span>Pick up where you left off</span></div>{recentPages.length ? <div className="page-grid">{recentPages.slice(0, 6).map(pageCard)}</div> : <div className="empty-panel"><Icon name="page" size={24} /><p>Open a page to see it here.</p><button className="btn" onClick={() => void openPageEditor()}>Write your first page</button></div>}</section>
        <section><div className="section-heading"><h2>All pages</h2><button className="btn quiet" onClick={() => navigate('templates')}>Start from a template<Icon name="arrow" size={16} /></button></div><div className="home-page-list">{pages.map(p => <button key={p.id} onClick={() => void openPageEditor(p)}><span>{p.icon || <Icon name="page" />}</span><strong>{p.title}</strong><span>Open page<Icon name="arrow" size={16} /></span></button>)}</div></section>
      </div>
    )

  if (view === 'page') {
    if (pageDatabaseId) {
      return <DatabaseView databaseId={pageDatabaseId} viewId={pageDatabaseViewId || undefined} onBack={() => { setPageDatabaseId(''); setPageDatabaseViewId(''); setView('home') }} onLinkedPageCreated={() => { void refresh() }} onPendingEditChange={pending => { databasePendingEditRef.current = pending }} onOpenRecord={pageId => { void (async () => { try { const full = await apiFetch<Page>(`/api/pages?id=${encodeURIComponent(pageId)}`); setSelectedPage(pageId); await openPageEditor(full) } catch (e) { setError((e as Error).message) } })() }} />
    }
    return (
      <div className={`page-wrap document-page ${fullWidth ? 'full-width' : ''}`} inert={pageRecoveryBusy}>
        <div className="row page-actions" style={{ justifyContent: 'space-between', marginBottom: 16 }}>
          <div className="breadcrumb" style={{ color: 'var(--muted)', fontSize: 13 }}>
            {(() => {
              const path: Page[] = []
              let cur = pageEditor
              while (cur) { path.unshift(cur); cur = pages.find((p) => p.id === cur?.parentId) || null }
              return path.map((p, i) => <span key={p.id}>{i > 0 && ' / '}{p.title}</span>)
            })()}
          </div>
          <div className="row">
            {pageEditor && <button className="btn" aria-label={library.favorites.includes(pageEditor.id) ? 'Remove from favorites' : 'Add to favorites'} aria-pressed={library.favorites.includes(pageEditor.id)} onClick={() => void toggleFavorite(pageEditor.id)}><Icon name="star" size={16} />{library.favorites.includes(pageEditor.id) ? 'Favorited' : 'Favorite'}</button>}
            <button className="btn" aria-pressed={fullWidth} onClick={() => setFullWidth(v => !v)}>Full width</button>
            {pageEditor && <button className="btn" disabled={pageRecoveryBusy} onClick={() => void openHistory()}>Page history</button>}
            {pageEditor && <button className="btn quiet" disabled={pageRecoveryBusy} onClick={() => void deletePage(pageEditor.id)}>Move to trash</button>}
            {pageEditor && <button className="btn" onClick={async () => {
              try {
                const db = await apiFetch<any>('/api/databases', { method: 'POST', body: JSON.stringify({ title: pageEditor.title || 'Untitled database', pageId: pageEditor.id }) })
                setPageDatabaseId(db.id)
              } catch (e) { setError((e as Error).message) }
            }}>Create database</button>}
          </div>
        </div>
        <button className="page-icon" aria-label="Change page icon" onClick={() => { if (!pageEditor) return; setIconDraft(pageEditor.icon || ''); setIconTarget(pageEditor) }}>{pageDraft.icon || '📄'}</button>
        <input className="page-title" aria-label="Page title" value={pageDraft.title} onChange={(e) => { pageTitleDirtyRef.current = true; setPageDraft((d) => ({ ...d, title: e.target.value })); pageTitleRef.current = e.target.value }} onBlur={() => { if (pageEditor && pageTitleDirtyRef.current && !pageLeaveError) void savePageContent(pageEditor.id, pageDraft.content || pageEditor.content).catch((e) => { if ((e as Error).message.includes('version conflict')) setPageLeaveError(true); else setError(`Title save failed: ${(e as Error).message}`) }) }} placeholder="Untitled" />
  {pageEditor && (
    <Suspense fallback={<p role="status">Loading editor…</p>}><PageEditor
      key={`${pageEditor.id}-${reloadToken}`}
      pageId={pageEditor.id}
      title={pageDraft.title}
      initialContent={pageEditor.content}
      theme={theme}
      reloadToken={reloadToken}
      discardRef={pageDiscardRef}
      initialStatus={pageLeaveError ? 'conflict' : 'saved'}
      showConflict={pageLeaveError}
      onSave={savePageContent}
      onContentChange={(c) => setPageDraft((d) => ({ ...d, content: c }))}
      onReload={async () => {
        pageDiscardRef.current = true
        try {
          const latest = await apiFetch<Page>(`/api/pages?id=${pageEditor.id}`)
          pageVersions.current.set(pageEditor.id, latest.version || 1)
          pageSnapshots.current.set(pageEditor.id, latest)
          pageTitleDirtyRef.current = false
          pageEditorRef.current = latest
          pageTitleRef.current = latest.title
          setPageEditor(latest)
          setPageDraft({ title: latest.title, icon: latest.icon ?? '', content: latest.content })
          setPageLeaveError(false)
          setReloadToken((t) => t + 1)
        } catch (e) { pageDiscardRef.current = false; setError((e as Error).message) }
      }}
      onRetry={async (content, retryBody) => {
        try {
          const latest = await apiFetch<Page>(`/api/pages?id=${pageEditor.id}`)
          pageSnapshots.current.set(pageEditor.id, latest)
          // Retry the local title only if it was edited here; a body-only retry
          // must retain a newer title written by another browser.
          const retryTitle = pageTitleDirtyRef.current ? pageTitleRef.current : latest.title
          const retryContent = retryBody ? content : latest.content
          const updated = await apiFetch<Page>('/api/pages', { method: 'PUT', body: JSON.stringify({ ...latest, title: retryTitle, content: retryContent, version: latest.version }) })
          if (typeof updated.version !== 'number' || updated.version <= (latest.version || 0)) {
            throw new Error('Page retry returned an invalid version')
          }
          pageVersions.current.set(pageEditor.id, updated.version)
          pageSnapshots.current.set(pageEditor.id, updated)
          if (pageEditorRef.current?.id === pageEditor.id) {
            pageEditorRef.current.version = updated.version
            pageEditorRef.current.title = updated.title
            pageEditorRef.current.icon = updated.icon
            pageEditorRef.current.cover = updated.cover
            pageEditorRef.current.parentId = updated.parentId
            pageEditorRef.current.content = updated.content
            if (pageTitleRef.current === retryTitle) {
              pageTitleRef.current = updated.title
              pageTitleDirtyRef.current = false
              setPageDraft(d => ({ ...d, title: updated.title }))
            }
          }
        } catch (e) { setError((e as Error).message); throw e }
      }}
      onStatus={handlePageSaveStatus}
    /></Suspense>
  )}
      </div>
    )
  }

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
        <WorkspaceStorage />
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
    <div className="app" data-theme={theme} onPointerDown={() => document.documentElement.dataset.input = 'pointer'} onKeyDown={() => document.documentElement.dataset.input = 'keyboard'}>
      <a className="skip-link" href="#main-content">Skip to content</a>
      {narrow ? !sidebarCollapsed && <Dialog label="Workspace navigation" navigation onClose={() => setSidebarCollapsed(true)}>{sidebar}<button className="icon-button navigation-close" aria-label="Close navigation" onClick={() => setSidebarCollapsed(true)}><Icon name="close" /></button></Dialog> : sidebar}
      {recovery && <PageRecovery pageId={recovery.pageId} version={recovery.version} returnFocus={recovery.returnFocus} onClose={() => setRecovery(null)} onRestored={async () => { if (recovery.pageId) await reloadRecoveredPage(); else await refresh() }} />}
      <main className="main">
        <header className="topbar">
          <button className="icon-button" aria-label="Toggle sidebar" aria-controls="workspace-sidebar" aria-expanded={!sidebarCollapsed} onClick={() => setSidebarCollapsed(c => !c)}><Icon name="sidebar" /></button>
          <nav className="breadcrumb" aria-label="Breadcrumb"><button onClick={() => navigate('home')} aria-label="Go to Home">Workspace</button><span>/</span><strong>{view === 'page' ? pageDraft.title : view.charAt(0).toUpperCase() + view.slice(1)}</strong></nav>
          <span className="topbar-spacer" />
          <button className="icon-button" aria-label="Quick find" onClick={() => setCmdOpen(true)}><Icon name="search" /></button>
          <button className="icon-button" aria-label={theme === 'light' ? 'Switch to dark theme' : 'Switch to light theme'} onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')}><Icon name={theme === 'light' ? 'moon' : 'sun'} /></button>
        </header>
        <div id="main-content" className="content" tabIndex={-1}>{renderContent()}</div>
      </main>

      {taskEditorOpen && (
        <Dialog label="Issue editor" onClose={() => setTaskEditorOpen(false)}>
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
        </Dialog>
      )}

      {projectEditorOpen && (
        <Dialog label="Project editor" onClose={() => setProjectEditorOpen(false)}>
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
        </Dialog>
      )}

      {iconTarget && (
        <Dialog label="Page icon" onClose={() => setIconTarget(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()} style={{ width: 320 }}>
            <h3>Page icon</h3>
            <input className="input" style={{ width: '100%', fontSize: 32, textAlign: 'center' }} value={iconDraft} onChange={(e) => setIconDraft(e.target.value)} autoFocus />
            <div className="row" style={{ justifyContent: 'flex-end', marginTop: 12 }}>
              <button className="btn" onClick={() => setIconTarget(null)}>Cancel</button>
              <button className="btn primary" onClick={async () => { if (!iconTarget) return; const ic = iconDraft; setPageDraft((d) => ({ ...d, icon: ic })); try { await apiFetch('/api/pages', { method: 'PUT', body: JSON.stringify({ ...iconTarget, icon: ic }) }); await refresh(); setIconTarget(null) } catch (e) { setError((e as Error).message) } }}>Save</button>
            </div>
          </div>
        </Dialog>
      )}
      {cmdOpen && (
        <Dialog label="Quick find" onClose={() => setCmdOpen(false)} instant>
          <div className="modal" onClick={(e) => e.stopPropagation()} style={{ width: 480 }}>
            <input className="input" style={{ width: '100%', marginBottom: 8 }} autoFocus placeholder="Search pages, projects, issues..." value={cmdQuery} onChange={(e) => setCmdQuery(e.target.value)} onKeyDown={(e) => {
              if (e.key === 'ArrowDown') { e.preventDefault(); setCmdIndex((i) => Math.min(i + 1, cmdResults.length - 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setCmdIndex((i) => Math.max(i - 1, 0)) }
              else if (e.key === 'Enter') { e.preventDefault(); cmdResults[cmdIndex]?.action() }
            }} />
            <div style={{ maxHeight: 320, overflowY: 'auto' }}>
              {cmdResults.map((r, i) => <button key={i} className="tree-item" style={{ background: i === cmdIndex ? 'var(--hover)' : 'transparent' }} onClick={r.action}>{r.label}</button>)}
              {cmdResults.length === 0 && <div className="empty">No results</div>}
            </div>
          </div>
        </Dialog>
      )}

      {error && <div className="error-toast" role="alert">{error}<button style={{ marginLeft: 8, background: 'transparent', border: 'none', color: 'white', cursor: 'pointer' }} aria-label="Dismiss error" onClick={() => setError('')}>×</button></div>}

    </div>
  )
}

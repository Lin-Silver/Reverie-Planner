import { useEffect, useMemo, useRef, useState } from 'react'
import type { DragEvent, FormEvent } from 'react'
import './App.css'

type ProjectStatus = 'active' | 'planning' | 'paused' | 'archived'
type TaskStatus = 'todo' | 'in_progress' | 'done' | 'blocked'
type Priority = 'low' | 'medium' | 'high'
type Project = {
  id: string
  name: string
  description: string
  status: ProjectStatus
  owner: string
  color: string
}
type Task = {
  id: string
  projectId: string
  title: string
  description?: string
  status: TaskStatus
  priority: Priority
  dueDate?: string
  assignee?: string
}
type Overview = {
  projectCount: number
  taskCount: number
  completedCount: number
  upcomingCount: number
  aiEnabled: boolean
}
type Provider = {
  id: string
  label: string
  provider: string
  baseURL: string
  model: string
  temperature: number
  maxTokens: number
  enabled: boolean
  streaming: boolean
  toolCalling: boolean
  apiKeyConfigured: boolean
}
type ProviderDraft = Omit<Provider, 'apiKeyConfigured'> & { apiKey: string }
type TaskDraft = { projectId: string; title: string; description: string; status: TaskStatus; priority: Priority; dueDate: string; assignee: string }
type ChatMessage = { role: 'user' | 'assistant'; content: string; id: string }
type Approval = {
  id: string
  sessionId: string
  toolName: string
  arguments: Record<string, unknown>
  summary: string
  highRisk: boolean
  status: string
}
type ChatResponse = {
  sessionId: string
  response: string
  pendingApprovals?: Approval[]
}
type Session = { id: string; title: string; providerId: string; updatedAt: string; messageCount: number }
type AuditEvent = { sessionId: string; action: string; details: string; createdAt: string }
type View = 'overview' | 'projects' | 'board' | 'assistant' | 'settings'

const statusColumns: { id: TaskStatus; label: string }[] = [
  { id: 'todo', label: '待办' },
  { id: 'in_progress', label: '进行中' },
  { id: 'done', label: '已完成' },
  { id: 'blocked', label: '阻塞中' },
]

const emptyProviderDraft: ProviderDraft = {
  id: '',
  label: '',
  provider: 'openai-compatible',
  baseURL: 'https://api.openai.com/v1',
  model: '',
  temperature: 0.3,
  maxTokens: 2048,
  enabled: false,
  streaming: true,
  toolCalling: true,
  apiKey: '',
}

async function apiFetch<T>(input: string, init?: RequestInit): Promise<T> {
  const response = await fetch(input, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
  })
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: 'Request failed' }))
    throw new Error((error as { error?: string }).error ?? 'Request failed')
  }
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

function App() {
  const [view, setView] = useState<View>('overview')
  const [projects, setProjects] = useState<Project[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [overview, setOverview] = useState<Overview | null>(null)
  const [providers, setProviders] = useState<Provider[]>([])
  const [sessions, setSessions] = useState<Session[]>([])
  const [auditEvents, setAuditEvents] = useState<AuditEvent[]>([])
  const [selectedProject, setSelectedProject] = useState('all')
  const [selectedProvider, setSelectedProvider] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [approvals, setApprovals] = useState<Approval[]>([])
  const [prompt, setPrompt] = useState('')
  const [providerDraft, setProviderDraft] = useState<ProviderDraft>(emptyProviderDraft)
  const [providerEditing, setProviderEditing] = useState(false)
  const [editingProject, setEditingProject] = useState<Project | null>(null)
  const [projectDraft, setProjectDraft] = useState({ name: '', description: '', status: 'planning' as ProjectStatus, owner: 'You', color: 'cyan' })
  const [editingTask, setEditingTask] = useState<Task | null>(null)
  const [taskDraft, setTaskDraft] = useState<TaskDraft>({ projectId: '', title: '', description: '', status: 'todo', priority: 'medium', dueDate: '', assignee: '' })
  const [taskEditorOpen, setTaskEditorOpen] = useState(false)
  const [projectFilter, setProjectFilter] = useState<'all' | 'active' | 'planning' | 'paused' | 'archived'>('all')
  const [search, setSearch] = useState('')
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => { try { return localStorage.getItem('reverie.sidebar.collapsed') === '1' } catch { return false } })
  const [dropTarget, setDropTarget] = useState<TaskStatus | ''>('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const controllerRef = useRef<AbortController | null>(null)

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && taskEditorOpen) setTaskEditorOpen(false)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [taskEditorOpen])
  const draggedTaskId = useRef('')

  const refresh = async () => {
    try {
      const [projectRows, taskRows, overviewData, providerRows, sessionRows, auditRows] = await Promise.all([
        apiFetch<Project[]>('/api/projects'),
        apiFetch<Task[]>('/api/tasks'),
        apiFetch<Overview>('/api/overview'),
        apiFetch<Provider[]>('/api/ai/providers'),
        apiFetch<Session[]>('/api/ai/sessions'),
        apiFetch<AuditEvent[]>('/api/ai/audit'),
      ])
      setProjects(projectRows)
      setTasks(taskRows)
      setOverview(overviewData)
      setProviders(providerRows)
      setSessions(sessionRows)
      setAuditEvents(auditRows)
      setSelectedProvider((current) => current || providerRows.find((provider) => provider.enabled)?.id || providerRows[0]?.id || '')
      setError('')
    } catch (err) {
      setError((err as Error).message)
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  useEffect(() => {
    try { localStorage.setItem('reverie.sidebar.collapsed', sidebarCollapsed ? '1' : '0') } catch {}
  }, [sidebarCollapsed])

  const selectedProjectData = projects.find((project) => project.id === selectedProject)
  const visibleProjects = useMemo(() => {
    const query = search.trim().toLowerCase()
    return projects.filter((project) => {
      const statusMatches = projectFilter === 'all' || project.status === projectFilter
      const queryMatches = !query || `${project.name} ${project.description} ${project.owner}`.toLowerCase().includes(query)
      return statusMatches && queryMatches
    })
  }, [projects, projectFilter, search])
  const visibleTasks = useMemo(() => tasks.filter((task) => {
    const projectMatches = selectedProject === 'all' || task.projectId === selectedProject
    const queryMatches = !search.trim() || `${task.title} ${task.description ?? ''}`.toLowerCase().includes(search.trim().toLowerCase())
    return projectMatches && queryMatches
  }), [tasks, selectedProject, search])

  const openProjectEditor = (project?: Project) => {
    setEditingProject(project ?? null)
    setProjectDraft(project
      ? { name: project.name, description: project.description, status: project.status, owner: project.owner, color: project.color }
      : { name: '', description: '', status: 'planning', owner: 'You', color: 'cyan' })
  }

  const saveProject = async (event: FormEvent) => {
    event.preventDefault()
    if (!projectDraft.name.trim()) return
    try {
      const updated = await apiFetch<Project>('/api/projects', {
        method: editingProject ? 'PUT' : 'POST',
        body: JSON.stringify({ ...projectDraft, id: editingProject?.id }),
      })
      setNotice(editingProject ? '项目已更新' : '项目已创建')
      setEditingProject(null)
      setProjectDraft({ name: '', description: '', status: 'planning', owner: 'You', color: 'cyan' })
      await refresh()
      if (!editingProject) setSelectedProject(updated.id)
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const setProjectStatus = async (project: Project, status: ProjectStatus) => {
    if (status === 'archived' && !window.confirm(`归档项目“${project.name}”？项目可随时恢复。`)) return
    try {
      await apiFetch<Project>('/api/projects', {
        method: 'PUT',
        body: JSON.stringify({ id: project.id, status }),
      })
      setNotice(status === 'archived' ? '项目已归档' : '项目已恢复')
      await refresh()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const openTaskEditor = (task?: Task) => {
    setEditingTask(task ?? null)
    setTaskEditorOpen(true)
    setTaskDraft(task
      ? {
          projectId: task.projectId,
          title: task.title,
          description: task.description ?? '',
          status: task.status,
          priority: task.priority,
          dueDate: task.dueDate ?? '',
          assignee: task.assignee ?? '',
        }
      : {
          projectId: selectedProject !== 'all' ? selectedProject : projects.find((project) => project.status !== 'archived')?.id ?? '',
          title: '',
          description: '',
          status: 'todo',
          priority: 'medium',
          dueDate: '',
          assignee: '',
        })
  }

  const saveTask = async (event: FormEvent) => {
    event.preventDefault()
    if (!taskDraft.title.trim() || !taskDraft.projectId) return
    try {
      await apiFetch<Task>('/api/tasks', {
        method: editingTask ? 'PUT' : 'POST',
        body: JSON.stringify({ ...taskDraft, id: editingTask?.id }),
      })
      setNotice(editingTask ? '任务已更新' : '任务已创建')
      setEditingTask(null)
      setTaskEditorOpen(false)
      await refresh()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const removeTask = async (task: Task) => {
    if (!window.confirm(`删除任务“${task.title}”？此操作无法撤销。`)) return
    try {
      await apiFetch<void>(`/api/tasks?id=${encodeURIComponent(task.id)}`, { method: 'DELETE' })
      setNotice('任务已删除')
      await refresh()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const moveTask = async (task: Task, status: TaskStatus) => {
    if (task.status === status) return
    try {
      await apiFetch<Task>('/api/tasks', { method: 'PUT', body: JSON.stringify({ id: task.id, status }) })
      await refresh()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const beginDrag = (event: DragEvent<HTMLElement>, taskId: string) => {
    draggedTaskId.current = taskId
    event.dataTransfer.setData('text/plain', taskId)
    event.dataTransfer.effectAllowed = 'move'
  }

  const endDrag = () => {
    draggedTaskId.current = ''
    setDropTarget('')
  }

  const dropOnColumn = (event: DragEvent<HTMLDivElement>, status: TaskStatus) => {
    event.preventDefault()
    const taskId = event.dataTransfer.getData('text/plain') || draggedTaskId.current
    const task = tasks.find((item) => item.id === taskId)
    if (task) void moveTask(task, status)
    endDrag()
  }

  const editProvider = (provider?: Provider) => {
    setProviderEditing(true)
    setProviderDraft(provider
      ? {
          id: provider.id,
          label: provider.label,
          provider: provider.provider,
          baseURL: provider.baseURL,
          model: provider.model,
          temperature: provider.temperature,
          maxTokens: provider.maxTokens,
          enabled: provider.enabled,
          streaming: provider.streaming,
          toolCalling: provider.toolCalling,
          apiKey: '',
        }
      : { ...emptyProviderDraft, id: `provider-${Date.now()}`, enabled: providers.length === 0 })
  }

  const saveProvider = async (event: FormEvent) => {
    event.preventDefault()
    try {
      const updated = await apiFetch<Provider[]>('/api/ai/providers', {
        method: 'POST',
        body: JSON.stringify([...providers.filter((provider) => provider.id !== providerDraft.id), providerDraft]),
      })
      setProviders(updated)
      setSelectedProvider(providerDraft.id)
      setProviderEditing(false)
      setProviderDraft(emptyProviderDraft)
      setNotice('Provider 配置已保存；密钥仅发送并保存在本机加密配置中。')
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const testProvider = async () => {
    if (!selectedProvider) return
    setBusy(true)
    try {
      const result = await apiFetch<{ ok: boolean; provider: string; model: string }>('/api/ai/test-connection', {
        method: 'POST',
        body: JSON.stringify({ providerId: selectedProvider }),
      })
      setNotice(`模型响应验证成功：${result.provider} · ${result.model}`)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const selectSession = async (id: string) => {
    try {
      const history = await apiFetch<Array<{ role: string; content: string }>>(`/api/ai/sessions/${encodeURIComponent(id)}`)
      setSessionId(id)
      setMessages(history.filter((item) => item.role === 'user' || item.role === 'assistant')
        .map((item, index) => {
          let content = item.content
          try {
            const parsed = JSON.parse(content) as { content?: string; role?: string }
            if (parsed.content !== undefined) content = parsed.content
          } catch {
            // Older sessions stored plain message content.
          }
          return { id: `${id}-${index}`, role: item.role as ChatMessage['role'], content }
        }))
      setApprovals(await apiFetch<Approval[]>(`/api/ai/approvals?sessionId=${encodeURIComponent(id)}`).catch(() => []))
      setView('assistant')
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const handleChat = async (event: FormEvent) => {
    event.preventDefault()
    if (!prompt.trim() || busy || !selectedProvider) return
    const content = prompt.trim()
    const userMessage: ChatMessage = { id: `user-${Date.now()}`, role: 'user', content }
    const assistantID = `assistant-${Date.now()}`
    setPrompt('')
    setBusy(true)
    setMessages((current) => [...current, userMessage, { id: assistantID, role: 'assistant', content: '' }])
    const controller = new AbortController()
    controllerRef.current = controller
    try {
      const response = await fetch('/api/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: content, sessionId, providerId: selectedProvider, streaming: true }),
        signal: controller.signal,
      })
      if (!response.ok) {
        const result = await response.json().catch(() => ({ error: 'Chat request failed' }))
        throw new Error(result.error ?? 'Chat request failed')
      }
      setSessionId((await readAgentStream(response, (delta) => {
        setMessages((current) => current.map((message) => message.id === assistantID ? { ...message, content: message.content + delta } : message))
      }, (approval) => {
        setApprovals((current) => current.some((item) => item.id === approval.id) ? current : [...current, approval])
      }, (result) => {
        setSessionId(result.sessionId)
        if (result.response) {
          setMessages((current) => current.map((message) => message.id === assistantID ? { ...message, content: result.response } : message))
        }
        setApprovals(result.pendingApprovals ?? [])
      }, controller.signal)))
      await refresh()
    } catch (err) {
      if ((err as Error).name === 'AbortError') {
        setMessages((current) => current.map((message) => message.id === assistantID
          ? { ...message, content: `${message.content}${message.content ? '\n' : ''}（已停止生成）` }
          : message))
      } else {
        setError((err as Error).message)
        setMessages((current) => current.map((message) => message.id === assistantID ? { ...message, content: (err as Error).message } : message))
      }
    } finally {
      controllerRef.current = null
      setBusy(false)
      void refresh()
    }
  }

  const resolveApproval = async (approval: Approval, approve: boolean, confirmText = '') => {
    try {
      const result = await apiFetch<ChatResponse>('/api/ai/approvals', {
        method: 'POST',
        body: JSON.stringify({ id: approval.id, approve, confirmText, streaming: false }),
      })
      setSessionId(result.sessionId)
      setApprovals(result.pendingApprovals ?? [])
      if (result.response) setMessages((current) => [...current, { id: `approval-${Date.now()}`, role: 'assistant', content: result.response }])
      setNotice(approve ? '操作已获批准并提交' : '操作已拒绝，未修改数据')
      await refresh()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const startNewSession = () => {
    setSessionId('')
    setMessages([])
    setApprovals([])
  }

  const currentProvider = providers.find((provider) => provider.id === selectedProvider)

  return (
    <div className="shell">
      <aside className={sidebarCollapsed ? 'sidebar collapsed' : 'sidebar'}>
        <div className="brand-block">
          <div className="brand-mark">R</div>
          {!sidebarCollapsed && <div>
            <p className="eyebrow">本地工作区</p>
            <h1>Reverie Planner</h1>
          </div>}
          <button type="button" className="sidebar-toggle" aria-label={sidebarCollapsed ? '展开侧边栏' : '折叠侧边栏'} aria-expanded={!sidebarCollapsed} onClick={() => setSidebarCollapsed((value) => !value)}>
            {sidebarCollapsed ? '›' : '‹'}
          </button>
        </div>
        <nav className="nav-section" aria-label="主导航">
          {([
            ['overview', '工作概览'],
            ['projects', '项目'],
            ['board', '任务看板'],
            ['assistant', 'AI 助手'],
            ['settings', '模型设置'],
          ] as [View, string][]).map(([id, label]) => (
            <button key={id} type="button" aria-label={label} className={view === id ? 'nav-item active' : 'nav-item'} onClick={() => setView(id)}>
              {!sidebarCollapsed && <span>{label}</span>}
              {id === 'projects' && <strong>{projects.filter((item) => item.status !== 'archived').length}</strong>}
              {id === 'assistant' && approvals.length > 0 && <strong>{approvals.length}</strong>}
            </button>
          ))}
        </nav>
        {!sidebarCollapsed && <div className="sidebar-panel">
          <div className="panel-head"><span>项目快捷入口</span><button type="button" className="ghost-btn" onClick={() => { openProjectEditor(); setView('projects') }}>新建</button></div>
          <div className="project-list">
            <button type="button" className={selectedProject === 'all' ? 'project-pill active' : 'project-pill'} onClick={() => setSelectedProject('all')}>
              <span className="dot dot-all" />全部项目
            </button>
            {projects.filter((project) => project.status !== 'archived').slice(0, 8).map((project) => (
              <button key={project.id} type="button" className={selectedProject === project.id ? 'project-pill active' : 'project-pill'} onClick={() => { setSelectedProject(project.id); setView('board') }}>
                <span className={`dot dot-${project.color}`} />{project.name}
              </button>
            ))}
          </div>
        </div>}
        <div className="sidebar-foot">
          <span className="status-indicator" />
          {!sidebarCollapsed && <span>本机 SQLite 工作区</span>}
        </div>
      </aside>

      <main className="main-panel">
        <header className="topbar">
          <div>
            <p className="eyebrow">REVERIE · PLANNER</p>
            <h2>{view === 'overview' ? '工作概览' : view === 'projects' ? '项目空间' : view === 'board' ? selectedProjectData?.name ?? '任务看板' : view === 'assistant' ? 'AI 助手' : '模型与连接'}</h2>
          </div>
          <div className="toolbar">
            <input className="search-input" aria-label="搜索" placeholder="搜索项目或任务…" value={search} onChange={(event) => setSearch(event.target.value)} />
            {view === 'board' && <button type="button" className="primary" onClick={() => openTaskEditor()}>新建任务</button>}
            {view === 'assistant' && <button type="button" className="toolbar-btn" onClick={startNewSession}>新会话</button>}
          </div>
        </header>

        {error && <div className="error-banner" role="alert">{error}<button type="button" onClick={() => setError('')}>关闭</button></div>}
        {notice && <div className="notice-banner" role="status">{notice}<button type="button" onClick={() => setNotice('')}>关闭</button></div>}
        {overview === null && <div className="loading-indicator">正在载入本地工作区…</div>}

        {view === 'overview' && overview && (
          <>
            <section className="metrics-row" aria-label="工作区指标">
              <Metric label="活跃项目" value={projects.filter((project) => project.status !== 'archived').length} detail={`${projects.filter((project) => project.status === 'archived').length} 个已归档`} />
              <Metric label="任务总量" value={overview.taskCount} detail={`${overview.completedCount} 个已完成`} />
              <Metric label="近期到期" value={overview.upcomingCount} detail="未完成并设置了截止日期" />
              <Metric label="可用模型" value={providers.filter((provider) => provider.enabled).length} detail={currentProvider?.label ?? '尚未选择 Provider'} />
            </section>
            <section className="content-grid">
              <div className="board-panel">
                <div className="panel-header"><div><p className="eyebrow">执行状态</p><h3>任务流</h3></div><button type="button" className="text-button" onClick={() => setView('board')}>打开看板 →</button></div>
                <div className="board">
                  {statusColumns.map((column) => <TaskColumn key={column.id} column={column} tasks={visibleTasks.filter((task) => task.status === column.id)} projects={projects} onEdit={openTaskEditor} onDelete={removeTask} onDrag={beginDrag} onDragEnd={endDrag} onDrop={dropOnColumn} onDragEnter={(status) => setDropTarget(status)} dropTarget={dropTarget} onStatus={moveTask} />)}
                </div>
              </div>
              <aside className="side-panel">
                <div className="panel-header"><div><p className="eyebrow">最近更新</p><h3>项目动态</h3></div></div>
                {projects.filter((project) => project.status !== 'archived').slice(0, 5).map((project) => (
                  <button key={project.id} className="project-summary" type="button" onClick={() => { setSelectedProject(project.id); setView('projects') }}>
                    <span className={`dot dot-${project.color}`} />
                    <span><strong>{project.name}</strong><small>{project.status} · {tasks.filter((task) => task.projectId === project.id).length} tasks</small></span>
                    <span aria-hidden>›</span>
                  </button>
                ))}
                {projects.length === 0 && <div className="empty-state">还没有项目，创建一个开始规划。</div>}
              </aside>
            </section>
          </>
        )}

        {view === 'projects' && (
          <section className="workspace-grid">
            <div className="board-panel">
              <div className="panel-header">
                <div><p className="eyebrow">项目组合</p><h3>{visibleProjects.length} 个项目</h3></div>
                <div className="filter-row">
                  <select aria-label="按状态筛选项目" value={projectFilter} onChange={(event) => setProjectFilter(event.target.value as typeof projectFilter)}>
                    <option value="all">全部状态</option><option value="active">进行中</option><option value="planning">规划中</option><option value="paused">已暂停</option><option value="archived">已归档</option>
                  </select>
                  <button type="button" className="primary" onClick={() => openProjectEditor()}>新建项目</button>
                </div>
              </div>
              <div className="project-grid">
                {visibleProjects.map((project) => (
                  <article className="project-card" key={project.id}>
                    <div className="project-card-top"><span className={`dot dot-${project.color}`} /><span className={`state-badge state-${project.status}`}>{projectStatusLabel(project.status)}</span></div>
                    <button type="button" className="project-card-title" onClick={() => { setSelectedProject(project.id); setView('board') }}>{project.name}</button>
                    <p>{project.description || '暂无项目描述'}</p>
                    <div className="project-card-meta"><span>{project.owner || '未分配负责人'}</span><span>{tasks.filter((task) => task.projectId === project.id).length} 个任务</span></div>
                    <div className="project-actions">
                      <button type="button" className="toolbar-btn" onClick={() => openProjectEditor(project)}>编辑</button>
                      {project.status === 'archived'
                        ? <button type="button" className="toolbar-btn" onClick={() => void setProjectStatus(project, 'active')}>恢复</button>
                        : <button type="button" className="toolbar-btn" onClick={() => void setProjectStatus(project, 'archived')}>归档</button>}
                    </div>
                  </article>
                ))}
              </div>
            </div>
            <aside className="side-panel">
              <div className="panel-header"><div><p className="eyebrow">{editingProject ? '编辑项目' : '快速创建'}</p><h3>{editingProject ? editingProject.name : '项目详情'}</h3></div></div>
              <form className="task-form" onSubmit={saveProject}>
                <label htmlFor="project-title">项目名称</label><input id="project-title" required value={projectDraft.name} onChange={(event) => setProjectDraft({ ...projectDraft, name: event.target.value })} />
                <label htmlFor="project-description">描述</label><textarea id="project-description" rows={3} value={projectDraft.description} onChange={(event) => setProjectDraft({ ...projectDraft, description: event.target.value })} />
                <label htmlFor="project-status">状态</label><select id="project-status" value={projectDraft.status} onChange={(event) => setProjectDraft({ ...projectDraft, status: event.target.value as ProjectStatus })}>
                  <option value="planning">规划中</option><option value="active">进行中</option><option value="paused">已暂停</option>{editingProject?.status === 'archived' && <option value="archived">已归档</option>}
                </select>
                <label htmlFor="project-owner">负责人</label><input id="project-owner" value={projectDraft.owner} onChange={(event) => setProjectDraft({ ...projectDraft, owner: event.target.value })} />
                <button type="submit" className="primary full-width">{editingProject ? '保存项目' : '创建项目'}</button>
                {editingProject && <button type="button" className="toolbar-btn full-width" onClick={() => openProjectEditor()}>取消编辑</button>}
              </form>
            </aside>
          </section>
        )}

        {view === 'board' && (
          <section className="board-panel">
            <div className="panel-header"><div><p className="eyebrow">拖放任务卡片以更新状态</p><h3>{visibleTasks.length} 个任务</h3></div>
              <select aria-label="选择项目" value={selectedProject} onChange={(event) => setSelectedProject(event.target.value)}><option value="all">全部项目</option>{projects.filter((project) => project.status !== 'archived').map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select>
            </div>
            <div className="board board-wide">{statusColumns.map((column) => <TaskColumn key={column.id} column={column} tasks={visibleTasks.filter((task) => task.status === column.id)} projects={projects} onEdit={openTaskEditor} onDelete={removeTask} onDrag={beginDrag} onDragEnd={endDrag} onDrop={dropOnColumn} onDragEnter={(status) => setDropTarget(status)} dropTarget={dropTarget} onStatus={moveTask} />)}</div>
            {taskEditorOpen && (
              <div className="modal-backdrop" role="presentation" onClick={() => setTaskEditorOpen(false)}>
                <div className="edit-dialog" role="dialog" aria-modal="true" aria-labelledby="task-dialog-title" onClick={(event) => event.stopPropagation()}>
                  <div className="panel-header"><div><p className="eyebrow">任务</p><h3 id="task-dialog-title">{editingTask?.title ?? '新建任务'}</h3></div><button type="button" className="text-button" onClick={() => setTaskEditorOpen(false)}>关闭</button></div>
                  <TaskEditor draft={taskDraft} setDraft={setTaskDraft} projects={projects} onSubmit={saveTask} editing={Boolean(editingTask)} />
                </div>
              </div>
            )}
            {!taskEditorOpen && (
              <div className="inline-create">
                <button type="button" className="toolbar-btn" onClick={() => openTaskEditor()}>＋ 新建任务</button>
              </div>
            )}
          </section>
        )}

        {view === 'assistant' && (
          <section className="assistant-layout">
            <aside className="session-panel">
              <div className="panel-header"><div><p className="eyebrow">历史记录</p><h3>会话</h3></div><button type="button" className="text-button" onClick={startNewSession}>新建</button></div>
              {sessions.map((session) => <button key={session.id} type="button" className={session.id === sessionId ? 'session-item active' : 'session-item'} onClick={() => void selectSession(session.id)}>
                <strong>{session.title || '新对话'}</strong><small>{session.messageCount} 条消息 · {new Date(session.updatedAt).toLocaleDateString()}</small>
              </button>)}
              {sessions.length === 0 && <p className="muted-copy">对话会自动保存在本机。</p>}
            </aside>
            <div className="chat-panel">
              <div className="chat-heading">
                <div><p className="eyebrow">OpenAI-compatible Agent</p><h3>与工作区协作</h3><small>{currentProvider ? `${currentProvider.label} · ${currentProvider.model}` : '请先在模型设置中配置 Provider'}</small></div>
                <select aria-label="选择 AI Provider" value={selectedProvider} onChange={(event) => setSelectedProvider(event.target.value)}>{providers.map((provider) => <option key={provider.id} value={provider.id}>{provider.label}{provider.enabled ? '' : '（已停用）'}</option>)}</select>
              </div>
              {currentProvider && !currentProvider.toolCalling && <div className="warning-banner">当前 Provider 已禁用工具调用，对话不会读取或修改项目数据。</div>}
              <div className="chat-transcript" aria-live="polite">
                {messages.length === 0 && <div className="chat-empty"><strong>你的项目工作台，随时可以提问。</strong><span>例如：“请创建一个项目，名称为中文项目。”写入操作在执行前都会请求批准。</span></div>}
                {messages.map((message) => <div key={message.id} className={`chat-message ${message.role}`}><span className="chat-role">{message.role === 'user' ? '你' : 'Reverie Agent'}</span><p>{message.content || (busy ? '正在思考…' : '')}</p></div>)}
              </div>
              {approvals.length > 0 && <div className="approval-list">
                <div className="panel-header"><div><p className="eyebrow">需要你确认</p><h3>待审批操作</h3></div></div>
                {approvals.map((approval) => <ApprovalCard key={approval.id} approval={approval} onResolve={resolveApproval} />)}
              </div>}
              <section className="audit-panel" aria-label="AI 操作历史">
                <div className="panel-header"><div><p className="eyebrow">审计记录</p><h3>最近工具执行</h3></div></div>
                {auditEvents.length === 0
                  ? <p className="muted-copy">尚无工具执行记录。</p>
                  : auditEvents.slice(0, 8).map((event, index) => <div className="audit-row" key={`${event.sessionId}-${event.createdAt}-${index}`}>
                    <strong>{event.details || event.action}</strong><span>{auditActionLabel(event.action)}</span><time>{new Date(event.createdAt).toLocaleString()}</time>
                  </div>)}
              </section>
              <form className="chat-composer" onSubmit={handleChat}>
                <label className="sr-only" htmlFor="chat-prompt">向 AI 助手提问</label>
                <textarea id="chat-prompt" rows={3} value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder="用中文描述你想查找或修改的项目…" disabled={busy || !selectedProvider} />
                <div className="composer-footer"><span>AI 写入需审批 · API Key 保存在本机，不会返回浏览器</span>{busy
                  ? <button type="button" className="toolbar-btn danger" onClick={() => controllerRef.current?.abort()}>停止生成</button>
                  : <button type="submit" className="primary" disabled={!selectedProvider || !prompt.trim()}>发送 ↑</button>}</div>
              </form>
            </div>
          </section>
        )}

        {view === 'settings' && (
          <section className="settings-layout">
            <div className="board-panel">
              <div className="panel-header"><div><p className="eyebrow">模型服务</p><h3>Provider 管理</h3></div><button type="button" className="primary" onClick={() => editProvider()}>添加 Provider</button></div>
              <p className="muted-copy">支持 OpenAI-compatible API 与本地 Ollama。远程地址要求 HTTPS；本地环回地址可使用 HTTP。密钥采用系统凭据保护加密存储。</p>
              <div className="provider-cards">{providers.map((provider) => <article key={provider.id} className={provider.id === selectedProvider ? 'provider-card selected' : 'provider-card'}>
                <div><strong>{provider.label}</strong><span className={`state-badge ${provider.enabled ? 'state-active' : 'state-paused'}`}>{provider.enabled ? '已启用' : '已停用'}</span></div>
                <p>{provider.model}</p><small>{provider.baseURL} · {provider.apiKeyConfigured ? '密钥已保存' : '未配置密钥'}</small>
                <div className="project-actions"><button type="button" className="toolbar-btn" onClick={() => { setSelectedProvider(provider.id); editProvider(provider) }}>编辑</button><button type="button" className="toolbar-btn" onClick={() => setSelectedProvider(provider.id)}>选择</button></div>
              </article>)}</div>
              {providerEditing && <form className="provider-form" onSubmit={saveProvider}>
                <div className="panel-header"><div><p className="eyebrow">Provider 配置</p><h3>{providerDraft.id}</h3></div><button type="button" className="text-button" onClick={() => setProviderEditing(false)}>取消</button></div>
                <div className="form-grid">
                  <label>显示名称<input required value={providerDraft.label} onChange={(event) => setProviderDraft({ ...providerDraft, label: event.target.value })} /></label>
                  <label>模型 ID<input required value={providerDraft.model} onChange={(event) => setProviderDraft({ ...providerDraft, model: event.target.value })} placeholder="gpt-4o-mini" /></label>
                  <label className="span-two">API Base URL<input required value={providerDraft.baseURL} onChange={(event) => setProviderDraft({ ...providerDraft, baseURL: event.target.value })} placeholder="https://api.openai.com/v1" /></label>
                  <label className="span-two">API Key<input type="password" autoComplete="new-password" value={providerDraft.apiKey} onChange={(event) => setProviderDraft({ ...providerDraft, apiKey: event.target.value })} placeholder={providers.find((provider) => provider.id === providerDraft.id)?.apiKeyConfigured ? '已保存；留空以保留当前密钥' : '仅在本次保存时发送'} /></label>
                  <label>Temperature<input type="number" min="0" max="2" step="0.1" value={providerDraft.temperature} onChange={(event) => setProviderDraft({ ...providerDraft, temperature: Number(event.target.value) })} /></label>
                  <label>最大输出 Token<input type="number" min="1" max="32768" value={providerDraft.maxTokens} onChange={(event) => setProviderDraft({ ...providerDraft, maxTokens: Number(event.target.value) })} /></label>
                </div>
                <div className="toggle-row"><label><input type="checkbox" checked={providerDraft.enabled} onChange={(event) => setProviderDraft({ ...providerDraft, enabled: event.target.checked })} />启用 Provider</label><label><input type="checkbox" checked={providerDraft.streaming} onChange={(event) => setProviderDraft({ ...providerDraft, streaming: event.target.checked })} />SSE 流式输出</label><label><input type="checkbox" checked={providerDraft.toolCalling} onChange={(event) => setProviderDraft({ ...providerDraft, toolCalling: event.target.checked })} />工具调用</label></div>
                <button type="submit" className="primary">安全保存</button>
              </form>}
              <button type="button" className="toolbar-btn" disabled={busy || !selectedProvider} onClick={() => void testProvider()}>{busy ? '正在验证模型响应…' : '发送测试提示并验证模型响应'}</button>
            </div>
            <aside className="side-panel">
              <div className="panel-header"><div><p className="eyebrow">本地运行</p><h3>安全与隐私</h3></div></div>
              <ul className="settings-facts"><li>业务数据持久化在项目目录的 SQLite 数据库中。</li><li>Provider 密钥不会由 GET 接口返回。</li><li>Windows 使用当前用户 DPAPI 加密密钥。</li><li>远程 Provider 必须 HTTPS，且 DNS 解析地址会被逐次校验。</li><li>Agent 只能调用注册的项目与任务工具，不执行任意 SQL。</li></ul>
              <div className="panel-header compact"><div><p className="eyebrow">当前状态</p><h3>{overview?.aiEnabled ? 'AI 功能已开启' : 'AI 功能已关闭'}</h3></div></div>
              <p className="muted-copy">模型能力验证会实际发送一个短提示并确认返回生成内容；不会只检查模型列表接口。</p>
            </aside>
          </section>
        )}
      </main>
    </div>
  )
}

async function readAgentStream(
  response: Response,
  onDelta: (content: string) => void,
  onApproval: (approval: Approval) => void,
  onDone: (result: ChatResponse) => void,
  signal: AbortSignal,
) {
  const contentType = response.headers.get('Content-Type') ?? ''
  if (!contentType.includes('text/event-stream')) {
    const result = await response.json() as ChatResponse
    onDone(result)
    return result.sessionId
  }
  if (!response.body) throw new Error('Streaming response body is unavailable')
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let session = ''
  while (!signal.aborted) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const frames = buffer.split(/\r?\n\r?\n/)
    buffer = frames.pop() ?? ''
    for (const frame of frames) {
      const event = frame.match(/^event:\s*(.+)$/m)?.[1]
      const data = frame.match(/^data:\s*(.+)$/m)?.[1]
      if (!data) continue
      const payload = JSON.parse(data) as Approval & ChatResponse & { content?: string; error?: string }
      if (event === 'delta') onDelta(payload.content ?? '')
      if (event === 'approval_required') onApproval(payload)
      if (event === 'done') {
        session = payload.sessionId
        onDone(payload)
      }
      if (event === 'error') throw new Error(payload.error ?? 'AI request failed')
    }
  }
  if (signal.aborted) throw new DOMException('Request aborted', 'AbortError')
  return session
}

function Metric({ label, value, detail }: { label: string; value: string | number; detail: string }) {
  return <div className="metric-card"><span className="metric-label">{label}</span><strong className="tabular-nums">{value}</strong><small>{detail}</small></div>
}

function TaskColumn({
  column,
  tasks,
  projects,
  onEdit,
  onDelete,
  onDrag,
  onDragEnd,
  onDrop,
  onDragEnter,
  dropTarget,
  onStatus,
}: {
  column: { id: TaskStatus; label: string }
  tasks: Task[]
  projects: Project[]
  onEdit: (task: Task) => void
  onDelete: (task: Task) => void
  onDrag: (event: DragEvent<HTMLElement>, taskId: string) => void
  onDragEnd: () => void
  onDrop: (event: DragEvent<HTMLDivElement>, status: TaskStatus) => void
  onDragEnter: (status: TaskStatus) => void
  dropTarget: TaskStatus | ''
  onStatus: (task: Task, status: TaskStatus) => void
}) {
  return <div className={dropTarget === column.id ? 'column drag-over' : 'column'} onDragOver={(event) => event.preventDefault()} onDragEnter={() => onDragEnter(column.id)} onDrop={(event) => onDrop(event, column.id)}>
    <div className="column-header"><span>{column.label}</span><strong>{tasks.length}</strong></div>
    <div className="task-stack">
      {tasks.map((task) => <article key={task.id} className="task-card" draggable onDragStart={(event) => onDrag(event, task.id)} onDragEnd={onDragEnd}>
        <div className="task-topline"><span className={`priority priority-${task.priority}`}>{task.priority}</span><span className="task-project">{projects.find((project) => project.id === task.projectId)?.name ?? '项目'}</span></div>
        <h4>{task.title}</h4><p>{task.description || '暂无描述'}</p>
        <div className="task-meta"><span>{task.assignee || '未分配'}</span><span>{task.dueDate || '无截止日期'}</span></div>
        <div className="task-actions"><button type="button" className="text-button" onClick={() => onEdit(task)}>编辑</button>
          <select aria-label={`更新 ${task.title} 状态`} value={task.status} onChange={(event) => onStatus(task, event.target.value as TaskStatus)}>{statusColumns.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}</select>
          <button type="button" className="text-button danger-text" onClick={() => onDelete(task)}>删除</button></div>
      </article>)}
      {tasks.length === 0 && <div className="empty-state">拖入任务，或在此状态创建任务。</div>}
    </div>
  </div>
}


function TaskEditor({
  draft,
  setDraft,
  projects,
  onSubmit,
  editing = false,
}: {
  draft: TaskDraft
  setDraft: (draft: TaskDraft) => void
  projects: Project[]
  onSubmit: (event: FormEvent) => void
  editing?: boolean
}) {
  return <form className="task-form form-grid" onSubmit={onSubmit}>
    <label className="span-two">任务名称<input required value={draft.title} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /></label>
    <label className="span-two">描述<textarea rows={3} value={draft.description} onChange={(event) => setDraft({ ...draft, description: event.target.value })} /></label>
    <label className="span-two">所属项目<select required value={draft.projectId} onChange={(event) => setDraft({ ...draft, projectId: event.target.value })}>{projects.filter((project) => project.status !== 'archived').map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label>
    <label>状态<select value={draft.status} onChange={(event) => setDraft({ ...draft, status: event.target.value as TaskStatus })}>{statusColumns.map((status) => <option key={status.id} value={status.id}>{status.label}</option>)}</select></label>
    <label>优先级<select value={draft.priority} onChange={(event) => setDraft({ ...draft, priority: event.target.value as Priority })}><option value="low">低</option><option value="medium">中</option><option value="high">高</option></select></label>
    <label>截止日期<input type="date" value={draft.dueDate} onChange={(event) => setDraft({ ...draft, dueDate: event.target.value })} /></label>
    <label>负责人<input value={draft.assignee} onChange={(event) => setDraft({ ...draft, assignee: event.target.value })} /></label>
    <button type="submit" className="primary span-two">{editing ? '保存任务' : '创建任务'}</button>
  </form>
}

function ApprovalCard({ approval, onResolve }: { approval: Approval; onResolve: (approval: Approval, approve: boolean, confirmText?: string) => void }) {
  const [confirmText, setConfirmText] = useState('')
  return <article className={approval.highRisk ? 'approval-card high-risk' : 'approval-card'}>
    <strong>{approval.summary}</strong>
    <p>{approval.highRisk ? '高影响操作：归档项目可恢复，但会影响项目的活跃列表。' : '此操作会写入本机数据库，执行前请核对参数。'}</p>
    <pre>{JSON.stringify(approval.arguments, null, 2)}</pre>
    {approval.highRisk && <label>输入 ARCHIVE 确认<input value={confirmText} onChange={(event) => setConfirmText(event.target.value)} /></label>}
    <div className="project-actions"><button type="button" className="primary" disabled={approval.highRisk && confirmText !== 'ARCHIVE'} onClick={() => onResolve(approval, true, confirmText)}>批准并执行</button><button type="button" className="toolbar-btn" onClick={() => onResolve(approval, false)}>拒绝</button></div>
  </article>
}

function projectStatusLabel(status: ProjectStatus) {
  return ({ active: '进行中', planning: '规划中', paused: '已暂停', archived: '已归档' })[status]
}

function auditActionLabel(action: string) {
  return ({
    approval_requested: '等待审批',
    tool_write: '已执行写入',
    tool_read: '只读工具',
    tool_denied: '操作已拒绝',
    tool_error: '工具错误',
  } as Record<string, string>)[action] ?? action
}

export default App

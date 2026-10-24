import { useEffect, useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import './App.css'

type Project = {
  id: string
  name: string
  description: string
  status: 'active' | 'planning' | 'paused'
  owner: string
  color: string
}

type Task = {
  id: string
  projectId: string
  title: string
  description?: string
  status: 'todo' | 'in_progress' | 'done' | 'blocked'
  priority: 'low' | 'medium' | 'high'
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
  enabled: boolean
  apiKey?: string
}

type AIChatMessage = {
  role: 'user' | 'assistant'
  content: string
}

type AIChatResponse = {
  sessionId: string
  response: string
  tools?: Array<{ name: string; summary: string; data?: unknown }>
}

const boardColumns = [
  { key: 'todo', label: '待办' },
  { key: 'in_progress', label: '进行中' },
  { key: 'done', label: '已完成' },
  { key: 'blocked', label: '阻塞中' },
] as const

async function apiFetch<T>(input: string, init?: RequestInit): Promise<T> {
  const response = await fetch(input, {
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
    ...init,
  })

  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: 'Request failed' }))
    throw new Error((error as { error?: string }).error ?? 'Request failed')
  }

  return (await response.json()) as T
}

function App() {
  const [projects, setProjects] = useState<Project[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [overview, setOverview] = useState<Overview>({
    projectCount: 0,
    taskCount: 0,
    completedCount: 0,
    upcomingCount: 0,
    aiEnabled: true,
  })
  const [providers, setProviders] = useState<Provider[]>([])
  const [selectedProject, setSelectedProject] = useState('all')
  const [newTaskTitle, setNewTaskTitle] = useState('')
  const [newProjectName, setNewProjectName] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [aiInput, setAiInput] = useState('list projects')
  const [aiMessages, setAiMessages] = useState<AIChatMessage[]>([
    { role: 'assistant', content: 'I can help manage projects and tasks. Try: list projects' },
  ])
  const [aiSessionId, setAiSessionId] = useState('')
  const [aiBusy, setAiBusy] = useState(false)

  const fetchData = async () => {
    try {
      setLoading(true)
      const [projectsResult, tasksResult, overviewResult, providersResult] = await Promise.all([
        apiFetch<Project[]>('/api/projects'),
        apiFetch<Task[]>('/api/tasks'),
        apiFetch<Overview>('/api/overview'),
        apiFetch<Provider[]>('/api/provider-settings'),
      ])

      setProjects(projectsResult)
      setTasks(tasksResult)
      setOverview(overviewResult)
      setProviders(providersResult)
      setSelectedProject('all')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void fetchData()
  }, [])

  const visibleTasks = useMemo(() => {
    if (selectedProject === 'all') return tasks
    return tasks.filter((task) => task.projectId === selectedProject)
  }, [selectedProject, tasks])

  const handleQuickTask = async (event: FormEvent) => {
    event.preventDefault()
    if (!newTaskTitle.trim()) return

    try {
      const created = await apiFetch<Task>('/api/tasks', {
        method: 'POST',
        body: JSON.stringify({
          projectId: selectedProject === 'all' ? projects[0]?.id ?? 'proj-galaxy' : selectedProject,
          title: newTaskTitle,
          status: 'todo',
          priority: 'medium',
          assignee: 'You',
        }),
      })
      setTasks((current) => [created, ...current])
      setNewTaskTitle('')
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const handleAIChat = async (event: FormEvent) => {
    event.preventDefault()
    if (!aiInput.trim() || aiBusy) return

    const prompt = aiInput.trim()
    setAiInput('')
    setAiBusy(true)
    setAiMessages((current) => [...current, { role: 'user', content: prompt }])

    try {
      const response = await apiFetch<AIChatResponse>('/api/ai/chat', {
        method: 'POST',
        body: JSON.stringify({
          message: prompt,
          sessionId: aiSessionId,
          providerId: providers[0]?.id ?? 'default',
        }),
      })
      setAiSessionId(response.sessionId)
      setAiMessages((current) => [...current, { role: 'assistant', content: response.response }])
      await fetchData()
    } catch (err) {
      setAiMessages((current) => [...current, { role: 'assistant', content: (err as Error).message }])
      setError((err as Error).message)
    } finally {
      setAiBusy(false)
    }
  }

  const handleProviderTest = async () => {
    if (!providers[0]) return
    try {
      const result = await apiFetch<{ ok: boolean; provider: string; model: string }>('/api/ai/test-connection', {
        method: 'POST',
        body: JSON.stringify({ providerId: providers[0].id }),
      })
      setError(result.ok ? `Provider ${result.provider} connected (${result.model})` : 'Provider test failed')
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const handleCreateProject = async (event: FormEvent) => {
    event.preventDefault()
    if (!newProjectName.trim()) return

    try {
      const created = await apiFetch<Project>('/api/projects', {
        method: 'POST',
        body: JSON.stringify({
          name: newProjectName,
          description: '新增项目，准备迭代规划。',
          status: 'planning',
          owner: 'You',
          color: 'cyan',
        }),
      })
      setProjects((current) => [created, ...current])
      setSelectedProject(created.id)
      setNewProjectName('')
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const selectedProjectMeta = selectedProject === 'all'
    ? { name: '全部项目', description: '跨项目视图' }
    : projects.find((project) => project.id === selectedProject) ?? { name: '项目', description: '详情' }

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand-block">
          <div className="brand-mark">R</div>
          <div>
            <p className="eyebrow">Workspace</p>
            <h1>Reverie Planner</h1>
          </div>
        </div>

        <nav className="nav-section" aria-label="Main navigation">
          <button type="button" className="nav-item active">
            <span>Overview</span>
            <strong>01</strong>
          </button>
          <button type="button" className="nav-item">
            <span>Projects</span>
            <strong>{projects.length}</strong>
          </button>
          <button type="button" className="nav-item">
            <span>Timeline</span>
            <strong>Q4</strong>
          </button>
          <button type="button" className="nav-item">
            <span>AI Assist</span>
            <strong>{overview.aiEnabled ? 'ON' : 'OFF'}</strong>
          </button>
        </nav>

        <div className="sidebar-panel">
          <div className="panel-head">
            <span>Projects</span>
            <button type="button" className="ghost-btn">New</button>
          </div>
          <div className="project-list">
            <button type="button" className={selectedProject === 'all' ? 'project-pill active' : 'project-pill'} onClick={() => setSelectedProject('all')}>
              <span className="dot dot-all" />
              全部项目
            </button>
            {projects.map((project) => (
              <button key={project.id} type="button" className={selectedProject === project.id ? 'project-pill active' : 'project-pill'} onClick={() => setSelectedProject(project.id)}>
                <span className={`dot dot-${project.color}`} />
                {project.name}
              </button>
            ))}
          </div>
        </div>

        <form className="quick-form" onSubmit={handleCreateProject}>
          <label htmlFor="project-name">新项目</label>
          <div className="input-row">
            <input id="project-name" value={newProjectName} onChange={(event) => setNewProjectName(event.target.value)} placeholder="Project name" />
            <button type="submit">+</button>
          </div>
        </form>
      </aside>

      <main className="main-panel">
        <header className="topbar">
          <div>
            <p className="eyebrow">Portfolio</p>
            <h2>{selectedProjectMeta.name}</h2>
          </div>
          <div className="toolbar">
            <button type="button" className="toolbar-btn">Filter</button>
            <button type="button" className="toolbar-btn primary">Create task</button>
          </div>
        </header>

        <section className="metrics-row" aria-label="Project overview metrics">
          <div className="metric-card">
            <span className="metric-label">Projects</span>
            <strong>{overview.projectCount}</strong>
            <small>+2 this month</small>
          </div>
          <div className="metric-card">
            <span className="metric-label">Task flow</span>
            <strong>{overview.taskCount}</strong>
            <small>{overview.completedCount} completed</small>
          </div>
          <div className="metric-card">
            <span className="metric-label">Upcoming</span>
            <strong>{overview.upcomingCount}</strong>
            <small>Due next 7 days</small>
          </div>
          <div className="metric-card">
            <span className="metric-label">AI</span>
            <strong>{overview.aiEnabled ? 'Ready' : 'Offline'}</strong>
            <small>{providers.length} providers</small>
          </div>
        </section>

        <section className="content-grid">
          <div className="board-panel">
            <div className="panel-header">
              <div>
                <p className="eyebrow">Board</p>
                <h3>Task pipeline</h3>
              </div>
              <span className="badge">{visibleTasks.length} items</span>
            </div>

            <div className="board">
              {boardColumns.map((column) => {
                const columnTasks = visibleTasks.filter((task) => task.status === column.key)
                return (
                  <div key={column.key} className="column">
                    <div className="column-header">
                      <span>{column.label}</span>
                      <strong>{columnTasks.length}</strong>
                    </div>
                    <div className="task-stack">
                      {columnTasks.length === 0 ? (
                        <div className="empty-state">No tasks</div>
                      ) : (
                        columnTasks.map((task) => (
                          <article key={task.id} className="task-card">
                            <div className="task-topline">
                              <span className={`priority priority-${task.priority}`}>{task.priority}</span>
                              <span className="task-project">{projects.find((project) => project.id === task.projectId)?.name ?? 'Project'}</span>
                            </div>
                            <h4>{task.title}</h4>
                            <p>{task.description ?? 'Task details ready for planning.'}</p>
                            <div className="task-meta">
                              <span>{task.assignee ?? 'Unassigned'}</span>
                              <span>{task.dueDate ?? 'No due date'}</span>
                            </div>
                          </article>
                        ))
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          </div>

          <aside className="side-panel">
            <div className="panel-header">
              <div>
                <p className="eyebrow">Quick add</p>
                <h3>New task</h3>
              </div>
            </div>

            <form className="task-form" onSubmit={handleQuickTask}>
              <label htmlFor="task-title">Task title</label>
              <input id="task-title" value={newTaskTitle} onChange={(event) => setNewTaskTitle(event.target.value)} placeholder="Design sprint review" />
              <label htmlFor="task-project">Project</label>
              <select id="task-project" value={selectedProject === 'all' ? projects[0]?.id ?? '' : selectedProject} onChange={(event) => setSelectedProject(event.target.value)}>
                {projects.map((project) => (
                  <option key={project.id} value={project.id}>{project.name}</option>
                ))}
              </select>
              <button type="submit" className="primary full-width">Save task</button>
            </form>

            <div className="panel-group">
              <div className="panel-header compact">
                <p className="eyebrow">AI</p>
                <h3>Assistant</h3>
              </div>
              <div className="agent-chat">
                {aiMessages.map((message, index) => (
                  <div key={`${message.role}-${index}`} className={`chat-bubble ${message.role}`}>
                    {message.content}
                  </div>
                ))}
              </div>
              <form onSubmit={handleAIChat} className="task-form">
                <label htmlFor="ai-input">Command</label>
                <textarea id="ai-input" value={aiInput} onChange={(event) => setAiInput(event.target.value)} rows={3} placeholder="list projects" />
                <button type="submit" className="primary full-width" disabled={aiBusy}>{aiBusy ? 'Thinking…' : 'Run'}</button>
              </form>
              <button type="button" className="toolbar-btn full-width" onClick={handleProviderTest}>Test provider</button>
            </div>

            <div className="panel-group">
              <div className="panel-header compact">
                <p className="eyebrow">AI</p>
                <h3>Providers</h3>
              </div>
              <ul className="provider-list">
                {providers.map((provider) => (
                  <li key={provider.id}>
                    <span>{provider.label}</span>
                    <strong>{provider.enabled ? 'Live' : 'Paused'}</strong>
                  </li>
                ))}
              </ul>
            </div>
          </aside>
        </section>

        {error && <div className="error-banner">{error}</div>}
        {loading && <div className="loading-indicator">Loading workspace…</div>}
      </main>
    </div>
  )
}

export default App

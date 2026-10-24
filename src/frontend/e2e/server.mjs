import { execFileSync, spawn } from 'node:child_process'
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const repositoryRoot = path.resolve(frontendDir, '..', '..')
const testRoot = await mkdtemp(path.join(os.tmpdir(), 'reverie-e2e-'))
const backendBinary = path.join(testRoot, 'runtime', `ReveriePlanner${process.platform === 'win32' ? '.exe' : ''}`)
const port = 18765
let backend
let stopping = false

async function shutdown() {
  if (stopping) return
  stopping = true
  try {
    const metadataPath = path.join(testRoot, 'runtime', 'service.json')
    const metadata = JSON.parse(await readFile(metadataPath, 'utf8'))
    await fetch(`http://127.0.0.1:${port}/api/shutdown`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${metadata.token}` },
      signal: AbortSignal.timeout(5000),
    })
  } catch {
    backend?.kill()
  }
  await new Promise((resolve) => {
    if (!backend || backend.exitCode !== null) return resolve()
    backend.once('exit', resolve)
    setTimeout(() => {
      if (backend.exitCode === null) backend.kill()
      resolve()
    }, 10_000).unref()
  })
  await rm(testRoot, { recursive: true, force: true })
}

async function prepare() {
  const configDir = path.join(testRoot, 'config')
  const staticDir = path.join(testRoot, 'src', 'frontend', 'dist')
  await mkdir(configDir, { recursive: true })
  await mkdir(staticDir, { recursive: true })
  await cp(path.join(frontendDir, 'dist'), staticDir, { recursive: true })
  await cp(path.join(repositoryRoot, 'config', 'providers.example.json'), path.join(configDir, 'providers.example.json'))
  await writeFile(path.join(configDir, 'app.json'), JSON.stringify({
    appName: 'Reverie Planner E2E',
    version: 'test',
    host: '127.0.0.1',
    port,
    theme: 'dark',
    features: { aiEnabled: true, localOnly: true },
  }))
}

try {
  await prepare()
  await mkdir(path.dirname(backendBinary), { recursive: true })
  execFileSync('go', ['build', '-o', backendBinary, './src/backend'], { cwd: repositoryRoot, stdio: 'inherit' })
  backend = spawn(backendBinary, ['--root', testRoot], { cwd: repositoryRoot, stdio: 'inherit' })
  backend.once('exit', async (code) => {
    if (!stopping) {
      console.error(`E2E backend exited unexpectedly with code ${code}`)
      await shutdown()
      process.exitCode = 1
    }
  })
  let ready = false
  for (let attempt = 0; attempt < 120; attempt++) {
    if (backend.exitCode !== null) throw new Error(`E2E backend exited with code ${backend.exitCode}`)
    try {
      const response = await fetch(`http://127.0.0.1:${port}/api/health`, { signal: AbortSignal.timeout(1000) })
      if (response.ok) {
        ready = true
        break
      }
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  if (!ready) throw new Error('E2E backend did not become ready.')
  console.log(`E2E backend ready at http://127.0.0.1:${port}`)
} catch (error) {
  console.error(error)
  await shutdown()
  process.exit(1)
}

process.once('SIGINT', () => void shutdown().then(() => process.exit(0)))
process.once('SIGTERM', () => void shutdown().then(() => process.exit(0)))

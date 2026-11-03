// Measures the real client-side database filter and sort modules over a
// captured API response. Invoked by scripts/benchmark.py after tsc compiles
// src/frontend/src/databaseFilters.ts and databaseSorting.ts.
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { pathToFileURL } from 'node:url'

function arg(name, fallback = undefined) {
  const index = process.argv.indexOf(name)
  return index >= 0 && process.argv[index + 1] ? process.argv[index + 1] : fallback
}

function percentile(values, fraction) {
  if (!values.length) return null
  const ordered = [...values].sort((a, b) => a - b)
  const index = Math.max(0, Math.min(ordered.length - 1, Math.ceil(fraction * ordered.length) - 1))
  return Number(ordered[index].toFixed(3))
}

function summarize(values) {
  const ordered = [...values].sort((a, b) => a - b)
  const total = values.reduce((sum, value) => sum + value, 0)
  return {
    count: values.length,
    totalMs: Number(total.toFixed(3)),
    meanMs: Number((total / values.length).toFixed(3)),
    p50Ms: percentile(values, 0.5),
    p95Ms: percentile(values, 0.95),
    minMs: Number(ordered[0].toFixed(3)),
    maxMs: Number(ordered[ordered.length - 1].toFixed(3)),
  }
}

function measure(fn, iterations, warmups = 3) {
  for (let index = 0; index < warmups; index += 1) fn()
  const latencies = []
  for (let index = 0; index < iterations; index += 1) {
    const started = process.hrtime.bigint()
    fn()
    latencies.push(Number(process.hrtime.bigint() - started) / 1e6)
  }
  return summarize(latencies)
}

const inputPath = arg('--input')
const library = arg('--lib')
const iterations = Number(arg('--iterations', '25'))
if (!inputPath || !library || !Number.isInteger(iterations) || iterations < 1) {
  console.error('usage: node scripts/filter_bench.mjs --input report.json --lib compiled-dir --iterations N')
  process.exit(2)
}

const input = JSON.parse(fs.readFileSync(inputPath, 'utf8'))
const filters = await import(pathToFileURL(path.join(library, 'src/frontend/src/databaseFilters.js')).href)
const sorting = await import(pathToFileURL(path.join(library, 'src/frontend/src/databaseSorting.js')).href)

const records = input.records
const properties = input.properties
const filterTree = input.filterTree
const sortRules = input.sortRules
const predicate = record => filters.matchesFilters(record, properties, filterTree)
const matched = records.filter(predicate).length

const result = {
  status: 'ok',
  records: records.length,
  iterations,
  matched,
  filter: {
    expression: filterTree,
    latencyMs: measure(() => records.filter(predicate), iterations),
  },
  sort: {
    rules: sortRules,
    latencyMs: measure(() => sorting.sortDatabaseRecords(records, properties, sortRules), iterations),
  },
  filterAndSort: {
    description: 'records.filter(matchesFilters) then sortDatabaseRecords',
    latencyMs: measure(
      () => sorting.sortDatabaseRecords(records.filter(predicate), properties, sortRules),
      iterations,
    ),
  },
}
process.stdout.write(JSON.stringify(result))

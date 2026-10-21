import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
describe('export route coverage',()=>{
  it.each([
    ['DashboardView','dashboard'],['BloggerDetailView','blogger'],['RecommendationDetailView','recommendation'],['SecuritiesView','securities'],['DocumentReportsView','documents'],['WorkbenchView','workbench'],['SimulationTradingView','trading'],
  ])('%s has the shared export entry for %s',(view,kind)=>{
    const source=readFileSync(resolve(import.meta.dirname,`../views/${view}.vue`),'utf8')
    expect(source).toContain('<PageExportButton');expect(source).toContain(`kind="${kind}"`)
  })
})

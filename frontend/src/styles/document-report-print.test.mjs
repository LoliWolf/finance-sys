import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

const stylesheet = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'main.css'), 'utf8')
let style
let fixture

function computed(selector) {
  return getComputedStyle(fixture.querySelector(selector))
}

describe('document report print styles', () => {
  beforeEach(() => {
    style = document.createElement('style')
    style.textContent = stylesheet
    document.head.append(style)
    // jsdom cannot paginate. Activate print rules to guard the layout contract;
    // actual page boundaries must also be checked with browser-generated PDFs.
    const printRules = Array.from(style.sheet.cssRules)
      .filter((rule) => rule.type === CSSRule.MEDIA_RULE && rule.conditionText === 'print')
      .flatMap((rule) => Array.from(rule.cssRules))
    style.textContent = stylesheet + '\n' + printRules.map((rule) => rule.cssText).join('\n')
    document.body.classList.add('document-report-printing')
    fixture = document.createElement('div')
    fixture.innerHTML = `
      <div class="document-report-detail">
        <section class="detail-hero report-cover"><h1>Report title</h1><p>Introduction</p><div class="detail-meta"><span>Author</span></div></section>
        <section class="metric-grid report-summary-grid">
          <article class="metric-card tone-accent"><div class="metric-label">Count</div><strong>8</strong><p>Note</p></article>
          <article class="metric-card"><strong>2</strong></article>
        </section>
        <section class="report-recommendation-list">
          <article class="panel report-recommendation-card">
            <header class="report-recommendation-header"><span class="avatar">A</span>Recommendation<div class="report-recommendation-meta"><span>2026-09-05</span><span class="direction-pill long">Long</span></div></header>
            <div class="report-current-line">Current</div>
            <div class="table-wrap"><table><thead><tr><th>Window</th></tr></thead><tbody><tr><td>5</td></tr></tbody></table></div>
            <div class="report-evidence-grid">
              <div><span class="report-section-label">Thesis</span><p>Long thesis</p></div>
              <details open><summary>Evidence</summary><div class="evidence-list">
                <div class="evidence-item"><small>Chunk 1</small>First evidence</div>
                <div class="evidence-item"><small>Chunk 2</small>Second evidence</div>
              </div></details>
            </div>
          </article>
        </section>
        <footer class="report-disclaimer"><strong>Methodology</strong><p>Disclaimer</p></footer>
      </div>`
    document.body.append(fixture)
  })

  afterEach(() => {
    fixture.remove()
    style.remove()
    document.body.classList.remove('document-report-printing')
  })

  it('uses block flow and keeps ordinary cards, table rows and evidence together', () => {
    for (const selector of ['.report-recommendation-list', '.report-evidence-grid', '.evidence-list']) {
      expect(computed(selector).display, selector).toBe('block')
    }
    for (const selector of ['.report-recommendation-card', '.report-current-line', '.table-wrap', 'tr', '.report-evidence-grid > div', '.evidence-item']) {
      expect(computed(selector).breakInside, selector).toBe('avoid')
      expect(computed(selector).pageBreakInside, selector).toBe('avoid')
    }
  })

  it('keeps headings with following content and leaves oversized evidence unclipped', () => {
    for (const selector of ['.report-recommendation-header', '.report-current-line', '.report-section-label', 'summary', '.evidence-item small']) {
      expect(computed(selector).breakAfter, selector).toBe('avoid')
    }
    expect(computed('thead').display).toBe('table-header-group')
    expect(computed('.panel').overflow).toBe('visible')
    expect(computed('.evidence-list').overflow).toBe('visible')
    expect(computed('.evidence-list').maxHeight).toBe('none')
    expect(computed('.document-report-detail').overflowWrap).toBe('anywhere')
    expect(computed('.document-report-detail').orphans).toBe('3')
    expect(computed('.document-report-detail').widows).toBe('3')
  })

  it('uses edge-to-edge warm paper with repeated content insets rather than white page margins', () => {
    // jsdom drops page-size descriptors; verify the declaration and check A4 in the PDF QA.
    expect(stylesheet).toMatch(/@page document-report\s*\{\s*size:\s*A4 portrait;\s*margin:\s*0;\s*\}/)
    expect(getComputedStyle(document.body).page).toBe('document-report')
    expect(getComputedStyle(document.body).backgroundColor).toBe('rgb(245, 241, 233)')
    expect(getComputedStyle(document.body).printColorAdjust).toBe('exact')
    expect(computed('.document-report-detail').padding).toBe('12mm 11mm')
    expect(computed('.document-report-detail').boxDecorationBreak).toBe('clone')
  })

  it('prints flat document sections without card backgrounds, rounded corners or shadows', () => {
    for (const selector of ['.detail-hero', '.metric-card', '.panel', '.evidence-item', '.report-disclaimer', '.detail-meta span', '.report-recommendation-meta > span']) {
      expect(computed(selector).borderRadius, selector).toBe('0px')
      expect(computed(selector).backgroundColor, selector).toBe('rgba(0, 0, 0, 0)')
    }
    for (const selector of ['.detail-hero', '.metric-card', '.panel', '.evidence-item', '.report-disclaimer']) {
      expect(computed(selector).boxShadow, selector).toBe('none')
      expect(computed(selector).borderRightWidth, selector).toBe('0px')
    }
    expect(computed('.avatar').display).toBe('none')
  })

  it('does not change the interactive screen layout', () => {
    document.body.classList.remove('document-report-printing')
    expect(computed('.report-recommendation-list').display).toBe('grid')
    expect(computed('.report-evidence-grid').display).toBe('grid')
    expect(computed('.evidence-list').display).toBe('grid')
    expect(computed('.evidence-list').maxHeight).toBe('300px')
    expect(computed('.detail-hero').borderRadius).toBe('19px')
    expect(computed('.panel').borderRadius).toBe('17px')
    expect(computed('.metric-card').borderRadius).toBe('16px')
    expect(computed('.document-report-detail').boxDecorationBreak).not.toBe('clone')
  })
})

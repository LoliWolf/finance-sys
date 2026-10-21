// The printed DOM must stay intact until afterprint (Safari can return from
// window.print before the dialog closes). The caller also cleans up on unmount.
export function beginReportPrint(title: string, onFinished: () => void): () => void {
  const previousTitle = document.title
  const alreadyPrinting = document.body.classList.contains('document-report-printing')
  let finished = false
  const cleanup = () => {
    if (finished) return
    finished = true
    window.removeEventListener('afterprint', cleanup)
    document.title = previousTitle
    if (!alreadyPrinting) document.body.classList.remove('document-report-printing')
    onFinished()
  }
  document.title = title.replace(/[\\/:*?"<>|]/g, '-').slice(0, 100)
  document.body.classList.add('document-report-printing')
  window.addEventListener('afterprint', cleanup)
  try {
    window.print()
  } catch (error) {
    cleanup()
    throw error
  }
  return cleanup
}

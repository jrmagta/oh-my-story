// Runs before first paint so the page never flashes the wrong theme.
// A choice saved by the nav toggle wins over the system preference.
try {
  var saved = localStorage.getItem('oms-theme')
  document.documentElement.dataset.theme =
    saved === 'blink-light' || saved === 'blink-dark' ? saved
      : matchMedia('(prefers-color-scheme: light)').matches ? 'blink-light' : 'blink-dark'
} catch (e) {
  document.documentElement.dataset.theme = 'blink-dark'
}

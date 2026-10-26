// Apply the saved mode before module loading; shared by main and vendor builds.
(function () {
  var mode = 'auto'
  try {
    mode = localStorage.getItem('theme') || 'auto'
  } catch {
    // Blocked browser storage falls back to the system preference.
  }
  var dark = mode === 'dark' ||
    (mode !== 'light' && typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', dark)
})()

export async function downloadFile(url: string, filename = 'download') {
  const cleanURL = url.trim()
  if (!cleanURL) return
  const cleanFilename = filenameFromURL(filename, filename || 'download')
  if (/^(blob:|data:)/i.test(cleanURL) || isSameOriginURL(cleanURL)) {
    downloadDirect(cleanURL, cleanFilename)
    return
  }
  try {
    await downloadViaBrowserFetch(cleanURL, cleanFilename)
    return
  } catch (error) {
    console.warn('[download] browser download failed, fallback to server proxy', error)
  }
  downloadViaServerProxy(cleanURL, cleanFilename)
}

async function downloadViaBrowserFetch(url: string, filename: string) {
  const response = await fetch(url, {
    mode: 'cors',
    credentials: 'omit',
    cache: 'force-cache',
  })
  if (!response.ok) throw new Error(`HTTP ${response.status}`)
  const blob = await response.blob()
  const objectURL = URL.createObjectURL(blob)
  try {
    downloadDirect(objectURL, filename)
  } finally {
    window.setTimeout(() => URL.revokeObjectURL(objectURL), 30_000)
  }
}

function downloadViaServerProxy(url: string, filename: string) {
  const params = new URLSearchParams({ url, filename })
  const frame = document.createElement('iframe')
  frame.src = `/api/download?${params.toString()}`
  frame.style.display = 'none'
  frame.setAttribute('aria-hidden', 'true')
  document.body.appendChild(frame)
  window.setTimeout(() => frame.remove(), 10 * 60_000)
}

export function filenameFromURL(url: string, fallback = 'download') {
  try {
    const pathname = new URL(url, window.location.href).pathname
    return decodeURIComponent(pathname.split('/').filter(Boolean).pop() || fallback)
  } catch {
    return fallback
  }
}

function downloadDirect(url: string, filename: string) {
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.rel = 'noopener noreferrer'
  document.body.appendChild(link)
  link.click()
  link.remove()
}

function isSameOriginURL(url: string) {
  try {
    return new URL(url, window.location.href).origin === window.location.origin
  } catch {
    return false
  }
}

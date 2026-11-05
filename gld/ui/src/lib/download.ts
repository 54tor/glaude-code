const OBJECT_URL_LIFETIME_MS = 1000

export function downloadText(fileName: string, text: string) {
  const downloadLink = document.createElement("a")
  downloadLink.href = URL.createObjectURL(new Blob([text], { type: "text/markdown" }))
  downloadLink.download = fileName
  downloadLink.click()
  setTimeout(() => URL.revokeObjectURL(downloadLink.href), OBJECT_URL_LIFETIME_MS)
}

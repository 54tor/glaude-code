// Flux NDJSON (une valeur JSON par ligne) lu au fil de l'eau : chaque ligne complète est rendue dès qu'elle arrive, une ligne
// coupée entre deux morceaux attend la suite, et la dernière ligne sans saut final n'est pas perdue.
export async function* readNdjson<T>(body: ReadableStream<Uint8Array>): AsyncGenerator<T> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let pendingText = ""
  for (;;) {
    const { value, done } = await reader.read()
    pendingText += decoder.decode(value, { stream: !done })
    const lines = pendingText.split("\n")
    if (done) pendingText = ""
    else pendingText = lines.pop() ?? ""
    for (const line of lines) {
      if (line.trim()) yield JSON.parse(line) as T
    }
    if (done) return
  }
}

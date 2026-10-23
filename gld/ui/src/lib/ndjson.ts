// Flux NDJSON (une valeur JSON par ligne) lu au fil de l'eau : chaque ligne complète est rendue dès qu'elle arrive, une ligne
// coupée entre deux morceaux attend la suite, et la dernière ligne sans saut final n'est pas perdue.
export async function* readNdjson<T>(body: ReadableStream<Uint8Array>): AsyncGenerator<T> {
  const reader = body.getReader()
  const dec = new TextDecoder()
  let buf = ""
  for (;;) {
    const { value, done } = await reader.read()
    buf += dec.decode(value, { stream: !done })
    const lines = buf.split("\n")
    buf = done ? "" : (lines.pop() ?? "")
    for (const l of lines) if (l.trim()) yield JSON.parse(l) as T
    if (done) return
  }
}

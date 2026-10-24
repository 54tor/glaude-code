const subseq = (s: string, q: string) => {
  let i = 0
  for (const c of s) if (c === q[i]) i++
  return i === q.length
}

// Sous-séquence insensible à la casse ; une correspondance dans le nom de fichier passe avant une correspondance dans le chemin.
export function rank(files: string[], query: string): string[] {
  const q = query.toLowerCase()
  if (!q) return files.slice(0, 50)
  const name: string[] = []
  const path: string[] = []
  for (const f of files) {
    const l = f.toLowerCase()
    if (subseq(l.slice(l.lastIndexOf("/") + 1), q)) name.push(f)
    else if (subseq(l, q)) path.push(f)
  }
  return name.concat(path).slice(0, 50)
}

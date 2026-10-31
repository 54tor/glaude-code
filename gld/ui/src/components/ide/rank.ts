const MAX_RESULTS = 50

const isSubsequence = (text: string, query: string) => {
  let matchedCount = 0
  for (const character of text) {
    if (character === query[matchedCount]) matchedCount++
  }
  return matchedCount === query.length
}

// Sous-séquence insensible à la casse ; une correspondance dans le nom de fichier passe avant une correspondance dans le chemin.
export function rank(files: string[], query: string): string[] {
  const lowerQuery = query.toLowerCase()
  if (!lowerQuery) return files.slice(0, MAX_RESULTS)
  const nameMatches: string[] = []
  const pathMatches: string[] = []
  for (const filePath of files) {
    const lowerPath = filePath.toLowerCase()
    const lowerName = lowerPath.slice(lowerPath.lastIndexOf("/") + 1)
    if (isSubsequence(lowerName, lowerQuery)) nameMatches.push(filePath)
    else if (isSubsequence(lowerPath, lowerQuery)) pathMatches.push(filePath)
  }
  return nameMatches.concat(pathMatches).slice(0, MAX_RESULTS)
}

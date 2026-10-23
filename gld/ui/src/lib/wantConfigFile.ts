// Fichier de réglages à présélectionner à la prochaine ouverture de Configuration (lu une fois, puis oublié).
export const wantConfigFile: { id: string; raw: boolean } = { id: "", raw: false }

export function takeConfigFile(): string {
  const id = wantConfigFile.id
  wantConfigFile.id = ""
  return id
}

// Ouvrir aussi l'éditeur texte des permissions (lu une fois, puis oublié).
export function takeConfigRaw(): boolean {
  const raw = wantConfigFile.raw
  wantConfigFile.raw = false
  return raw
}

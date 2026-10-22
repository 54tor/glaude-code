// Fichier de réglages à présélectionner à la prochaine ouverture de Configuration (lu une fois, puis oublié).
export const wantConfigFile: { id: string } = { id: "" }

export function takeConfigFile(): string {
  const id = wantConfigFile.id
  wantConfigFile.id = ""
  return id
}

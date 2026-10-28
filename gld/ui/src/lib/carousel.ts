// Pas d'un cran dans la file du carrousel, en boucle ; la session lue n'y figure plus quand on vient d'y répondre.
export function stepIn<T>(list: T[], isCur: (x: T) => boolean, d: 1 | -1): T | undefined {
  const rest = list.filter((x) => !isCur(x))
  if (!rest.length) return undefined
  const i = list.findIndex(isCur)
  if (i < 0) return d > 0 ? rest[0] : rest[rest.length - 1]
  return list[(i + d + list.length) % list.length]
}

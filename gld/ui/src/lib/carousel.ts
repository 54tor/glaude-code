// Pas d'un cran dans la file du carrousel, en boucle ; la session lue n'y figure plus quand on vient d'y répondre.
export function stepIn<T>(list: T[], isCur: (item: T) => boolean, direction: 1 | -1): T | undefined {
  const otherItems = list.filter((item) => !isCur(item))
  if (otherItems.length === 0) return undefined
  const currentIndex = list.findIndex(isCur)
  if (currentIndex < 0) {
    if (direction > 0) return otherItems[0]
    return otherItems[otherItems.length - 1]
  }
  const nextIndex = (currentIndex + direction + list.length) % list.length
  return list[nextIndex]
}

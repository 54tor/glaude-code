// Chemins de projet (Unix ou Windows) : découpage relatif au dossier personnel, et jonction.

const WINDOWS_DRIVE_PREFIX = /^[A-Za-z]:\//

const toForwardSlashes = (path: string) => path.replace(/\\/g, "/")
const startsWithWindowsDrive = (path: string) => WINDOWS_DRIVE_PREFIX.test(path)

// ["~", ...] sous le home ; ["/", ...] ailleurs sous Unix ; ["C:", ...] sous Windows ; ["?", nom] si le dossier est inconnu.
export function segs(project: string, home: string): string[] {
  const projectPath = toForwardSlashes(project)
  const homePath = toForwardSlashes(home)
  const isWindowsPath = startsWithWindowsDrive(projectPath)
  const isAbsoluteUnixPath = projectPath.startsWith("/")
  if (!isAbsoluteUnixPath && !isWindowsPath) return ["?", project]

  const samePath = (left: string, right: string) => {
    if (isWindowsPath) return left.toLowerCase() === right.toLowerCase()
    return left === right
  }
  if (samePath(projectPath, homePath)) return ["~"]

  const isUnderHome = samePath(projectPath.slice(0, homePath.length), homePath) && projectPath[homePath.length] === "/"
  if (isUnderHome) {
    const relativeSegments = projectPath.slice(homePath.length + 1).split("/").filter(Boolean)
    return ["~", ...relativeSegments]
  }

  const pathSegments = projectPath.split("/").filter(Boolean)
  if (isWindowsPath) return pathSegments
  return ["/", ...pathSegments]
}

// Jonction d'un dossier et d'un nom avec le séparateur du système ; sous Windows la liste des lecteurs donne des chemins complets.
export function join(dir: string, name: string, sep: string): string {
  if (dir === "") return name
  if (dir.endsWith(sep)) return dir + name
  return dir + sep + name
}

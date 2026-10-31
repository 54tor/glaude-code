const ENTITIES: [RegExp, string][] = [
  [/&lt;/g, "<"],
  [/&gt;/g, ">"],
  [/&quot;/g, '"'],
  [/&(?:#39|apos);/g, "'"],
  [/&amp;/g, "&"],
]

// Les valeurs d'une <task-notification> arrivent échappées en XML : « && » y est écrit « &amp;&amp; ».
export const decodeEntities = (text: string) => ENTITIES.reduce((decoded, [pattern, replacement]) => decoded.replace(pattern, replacement), text)

const USAGE_FIELD = /<([a-z][a-z0-9_-]*)>([^<]*)<\/\1>/g

// <usage> : une suite de <nom>valeur</nom> (jetons, appels d'outils, durée).
export function parseUsage(body: string): [name: string, value: string][] {
  return [...body.matchAll(USAGE_FIELD)].map(([, fieldName, fieldValue]) => [fieldName, fieldValue.trim()])
}

const MILLISECONDS_PER_SECOND = 1000
const SECONDS_PER_MINUTE = 60

export function formatDuration(milliseconds: number) {
  const totalSeconds = Math.round(milliseconds / MILLISECONDS_PER_SECOND)
  if (totalSeconds < SECONDS_PER_MINUTE) return `${totalSeconds} s`
  return `${Math.floor(totalSeconds / SECONDS_PER_MINUTE)} min ${totalSeconds % SECONDS_PER_MINUTE} s`
}

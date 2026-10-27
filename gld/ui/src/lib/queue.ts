// Message mis en file : tant que d'autres suivent, la session est priée de ne pas s'arrêter sur des questions mais de les garder
// pour la fin. Idempotent : un message remis en tête de file après un échec ne reçoit pas le préfixe deux fois.
export const FOLLOW_PREFIX = "[Message en file d'attente : d'autres messages suivent. Ne t'arrête pas pour poser une question : note tes éventuelles questions et pose-les à la fin, une fois tous les messages traités.]\n\n"

export const withFollowUps = (prompt: string, more: boolean) => (more && !prompt.startsWith(FOLLOW_PREFIX) ? FOLLOW_PREFIX + prompt : prompt)
export const withoutFollowUps = (prompt: string) => (prompt.startsWith(FOLLOW_PREFIX) ? prompt.slice(FOLLOW_PREFIX.length) : prompt)

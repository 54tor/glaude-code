// Préfixe posé par le serveur sur un message de file quand d'autres suivent (followPrefix de gld/queue_routes.go, un test Go
// compare les deux) : l'interface le retire pour afficher le texte de l'utilisateur.
export const FOLLOW_PREFIX = "[Message en file d'attente : d'autres messages suivent. Ne t'arrête pas pour poser une question : note tes éventuelles questions et pose-les à la fin, une fois tous les messages traités.]\n\n"

export const withoutFollowUps = (prompt: string) => (prompt.startsWith(FOLLOW_PREFIX) ? prompt.slice(FOLLOW_PREFIX.length) : prompt)

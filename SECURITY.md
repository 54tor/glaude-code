# Politique de sécurité

## Signaler une faille

N'ouvrez pas d'issue publique. Utilisez le signalement privé de GitHub : onglet **Security** du dépôt, puis **Report a vulnerability**.

Indiquez la version concernée, les étapes pour reproduire, l'impact estimé et, si possible, une correction proposée. Ne joignez ni jeton, ni cookie, ni donnée personnelle réelle.

## Ce que vous pouvez attendre

- Accusé de réception sous quelques jours.
- Correction dans une version de correction, puis publication de l'avis une fois le correctif disponible.
- Mention dans le changelog si vous le souhaitez.

## Versions prises en charge

Seule la dernière version publiée reçoit des correctifs de sécurité.

## Périmètre

Sont concernés : l'authentification et le second facteur, les droits par compte, les liens d'invitation, l'écriture de fichiers (liens symboliques, sortie de dossier), le WebSocket et l'API `/v1`, et la fuite de données entre comptes. L'exposition directe sur Internet sans reverse proxy ni HTTPS n'est pas un cas pris en charge.

// Régénère gld/auth_commonpw.txt (mots de passe courants refusés) depuis @zxcvbn-ts/language-common.
import { writeFileSync } from "node:fs"
import { dictionary } from "@zxcvbn-ts/language-common"

const target = process.argv[2] ?? new URL("../../auth_commonpw.txt", import.meta.url)
const passwords = [...new Set(dictionary["passwords-common"].map(String))].filter((password) => password.trim() !== "" && !/[\r\n]/.test(password))
writeFileSync(target, passwords.join("\n") + "\n")
console.log(`${passwords.length} mots de passe écrits`)

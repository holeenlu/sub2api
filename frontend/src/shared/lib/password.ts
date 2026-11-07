// Shared by administrator-created customer and supplier login accounts.
export function generateRandomPassword(): string {
  const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%^&*'
  const ceiling = 256 - (256 % alphabet.length)
  let password = ''
  while (password.length < 16) {
    for (const byte of crypto.getRandomValues(new Uint8Array(32))) {
      if (byte >= ceiling) continue
      password += alphabet[byte % alphabet.length]
      if (password.length === 16) break
    }
  }
  return password
}

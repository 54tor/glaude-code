// Service worker : cache des ressources statiques de l'interface et clic sur les notifications.
// Jamais d'API ici : seules les ressources versionnées (/assets/, icônes) passent par le cache ; le manifeste va au réseau, iOS en lit les icônes.
const CACHE = "gld-static-v4" // le changer purge l'ancien cache à l'activation (v4 : icônes PWA remplacées)
const STATIC = (u) => u.pathname.startsWith("/assets/") || u.pathname.startsWith("/icons/") || u.pathname === "/favicon.ico"

self.addEventListener("install", () => self.skipWaiting())
self.addEventListener("activate", (e) => e.waitUntil(caches.keys().then((ks) => Promise.all(ks.filter((k) => k !== CACHE).map((k) => caches.delete(k)))).then(() => self.clients.claim())))

self.addEventListener("fetch", (e) => {
  const u = new URL(e.request.url)
  if (e.request.method !== "GET" || u.origin !== location.origin || !STATIC(u)) return
  // /assets/ porte un hash dans son nom : le cache est définitif. Le reste est servi du cache puis rafraîchi.
  const hashed = u.pathname.startsWith("/assets/")
  e.respondWith(
    caches.open(CACHE).then(async (c) => {
      const hit = await c.match(e.request)
      const net = () =>
        fetch(e.request).then((r) => {
          if (r.ok) c.put(e.request, r.clone())
          return r
        })
      if (hit) return hashed ? hit : (net().catch(() => {}), hit)
      return net()
    })
  )
})

self.addEventListener("notificationclick", (e) => {
  e.notification.close()
  const url = e.notification.data?.url || "/"
  e.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((cs) => {
      const c = cs[0]
      return c ? c.focus().then(() => c.navigate(url)) : self.clients.openWindow(url)
    })
  )
})

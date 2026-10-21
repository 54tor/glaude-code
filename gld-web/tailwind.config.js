module.exports = {
  content: ['./page.html'],
  darkMode: 'media',
  theme: { extend: { colors: Object.fromEntries(
    ['background','foreground','card','muted','muted-foreground','border','accent','destructive','ring']
      .map(k => [k, `hsl(var(--${k}))`])) } },
}

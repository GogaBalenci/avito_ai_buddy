/** Главный экран стажёра: чат + панель «Мой путь» (наполняется в волнах 3–4). */
export function HomePage() {
  return (
    <main className="home">
      <section className="home__chat card" aria-label="Чат с Витей" />
      <aside className="home__path card" aria-label="Мой путь" />
    </main>
  )
}

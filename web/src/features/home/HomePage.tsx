import { MyPathPanel } from '@/features/path/MyPathPanel'
import { ChatPane } from '@/features/chat/ChatPane'

/** Главный экран стажёра: чат + панель «Мой путь» (11 §1.6, шаг 1). */
export function HomePage() {
  return (
    <main className="home">
      <section className="home__chat card" aria-label="Чат с Витей">
        <ChatPane />
      </section>
      <aside className="home__path card" aria-label="Мой путь">
        <MyPathPanel />
      </aside>
    </main>
  )
}

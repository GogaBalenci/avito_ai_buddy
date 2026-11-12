import { MyPathPanel } from '@/features/path/MyPathPanel'
import { ChatPane } from '@/features/chat/ChatPane'
import { MyRequestsBlock } from '@/features/requests/requests'

/** Главный экран стажёра: чат + панель «Мой путь» с блоком «Мои заявки» (11 §1.6, 10 §12.2). */
export function HomePage() {
  return (
    <main className="home">
      <section className="home__chat card" aria-label="Чат с Витей">
        <ChatPane />
      </section>
      <aside className="home__path card" aria-label="Мой путь">
        <MyPathPanel>
          <MyRequestsBlock />
        </MyPathPanel>
      </aside>
    </main>
  )
}

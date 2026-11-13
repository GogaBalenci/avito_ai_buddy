// Маленькие праздники (09 §17.8, AG-34): закрыт последний шаг этапа →
// Витя в done и тост. Один раз на этап за сессию вкладки: «Отменить» → снова
// «Готово» праздник не повторяет.
import type { PathView } from '@/shared/api/schemas'
import { useMascot } from '@/shared/mascot/mascotStore'
import { toast } from '@/shared/ui/toastStore'
import { registerClientReset } from '@/app/session'
import { celebrationText, completedPeriods } from './logic'

const celebrated = new Set<string>()

/** Сравнивает путь до и после подтверждённого сервером изменения. */
export function celebrate(prev: PathView | undefined, next: PathView | undefined): void {
  if (!next) return
  for (const p of completedPeriods(prev, next)) {
    if (celebrated.has(p.id)) continue
    celebrated.add(p.id)
    useMascot.getState().setClient('done')
    toast(celebrationText(p), { tone: 'success', durationMs: 8000 })
  }
}

/** Вход другой учёткой, выход, «Сбросить демо-данные» — праздники снова доступны. */
export function resetCelebrations(): void {
  celebrated.clear()
}
registerClientReset(resetCelebrations)

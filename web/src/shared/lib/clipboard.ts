/**
 * Копирует текст. Если буфер обмена недоступен (браузер запретил) — возвращает false,
 * и UI выделяет текст с подсказкой «Скопируй вручную: Ctrl+C» (09 §8.2).
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (!navigator.clipboard?.writeText) return false
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

export const MANUAL_COPY_HINT = 'Скопируй вручную: Ctrl+C'

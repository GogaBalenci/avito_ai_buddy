import type { ReactNode } from 'react'

type Props = { legend: string; caption?: ReactNode; captionId?: string; error?: boolean; children: ReactNode }

/** Паттерн fieldset: заголовок, поля, подпись (меняется на текст ошибки). */
export function Fieldset({ legend, caption, captionId, error = false, children }: Props) {
  return (
    <fieldset className="fieldset">
      <legend className="fieldset__legend">{legend}</legend>
      <div className="fieldset__fields">{children}</div>
      {caption && (
        <p id={captionId} className={error ? 'fieldset__caption fieldset__caption--error' : 'fieldset__caption'}>
          {caption}
        </p>
      )}
    </fieldset>
  )
}

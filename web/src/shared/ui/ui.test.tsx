import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Button } from './Button'
import { Chip, Plaque } from './Chip'
import { ErrorState } from './ErrorState'
import { Fieldset } from './Fieldset'
import { Skeleton } from './Skeleton'
import { Toaster } from './Toaster'
import { toast, useToasts } from './toastStore'
import { Vitya, type VityaState } from '../mascot/Vitya'
import { selectMascot, useMascot } from '../mascot/mascotStore'

describe('ui kit', () => {
  it('Button shows loading and is disabled', () => {
    render(<Button variant="primary" loading>Проверить</Button>)
    const b = screen.getByRole('button', { name: 'Проверить' })
    expect(b).toBeDisabled()
    expect(b).toHaveAttribute('aria-busy', 'true')
    expect(b.className).toContain('btn--primary')
  })

  it('Chip, Plaque, Skeleton, Fieldset, ErrorState render', async () => {
    const retry = vi.fn()
    render(
      <>
        <Chip tone="success" icon="✓">Готово</Chip>
        <Plaque>Тестовая инструкция</Plaque>
        <Skeleton lines={2} />
        <Fieldset legend="Даты отгула" caption="Ошибка" captionId="c1" error>
          <input aria-describedby="c1" />
        </Fieldset>
        <ErrorState text="Не получилось" onRetry={retry} />
      </>,
    )
    expect(screen.getByText('Готово')).toBeInTheDocument()
    expect(screen.getByText('Тестовая инструкция')).toBeInTheDocument()
    expect(screen.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
    expect(screen.getByText('Ошибка').className).toContain('error')
    await userEvent.click(screen.getByRole('button', { name: 'Повторить' }))
    expect(retry).toHaveBeenCalled()
  })

  it('Toaster shows, runs an action and auto-dismisses', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const undo = vi.fn()
    render(<Toaster />)
    act(() => {
      toast('Отмечено', { action: { label: 'Отменить', onClick: undo }, tone: 'success' })
      toast('Ошибка', { tone: 'error' })
    })
    expect(screen.getByRole('alert')).toHaveTextContent('Ошибка')
    await userEvent.click(screen.getByRole('button', { name: 'Отменить' }))
    expect(undo).toHaveBeenCalled()
    expect(screen.queryByText('Отмечено')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть уведомление' }))
    act(() => {
      toast('Временный', { durationMs: 1000 })
    })
    act(() => vi.advanceTimersByTime(1100))
    expect(screen.queryByText('Временный')).not.toBeInTheDocument()
    act(() => useToasts.getState().clear())
    vi.useRealTimers()
  })

  it.each<VityaState>(['hello', 'thinking', 'done', 'oops', 'ask_mentor'])('Vitya renders state %s', (s) => {
    render(<Vitya state={s} />)
    expect(screen.getByRole('img')).toHaveAttribute('data-state', s)
  })

  it('mascot store: thinking from client wins, server resets client override', () => {
    const st = useMascot.getState()
    st.setServer('done')
    expect(selectMascot(useMascot.getState(), false)).toBe('done')
    st.setClient('oops')
    expect(selectMascot(useMascot.getState(), false)).toBe('oops')
    expect(selectMascot(useMascot.getState(), true)).toBe('thinking')
    st.setServer('hello')
    expect(useMascot.getState().client).toBeNull()
    st.reset()
    expect(useMascot.getState().server).toBe('hello')
  })
})

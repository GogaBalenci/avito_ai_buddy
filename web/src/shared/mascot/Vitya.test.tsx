import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { render } from '@testing-library/react'
import { Vitya, type VityaState } from './Vitya'

const css = readFileSync(resolve(process.cwd(), 'src/shared/styles/base.css'), 'utf8')

describe('Vitya: live states (09 §17.7, AG-33)', () => {
  it.each<VityaState>(['hello', 'thinking', 'done', 'oops', 'ask_mentor'])('%s: state class, tail and ears', (state) => {
    const { container } = render(<Vitya state={state} />)
    const svg = container.querySelector('svg')!
    expect(svg).toHaveClass('vitya', `vitya--${state}`)
    expect(svg.querySelector('.vitya__tail')).not.toBeNull()
    expect(svg.querySelectorAll('.vitya__ear')).toHaveLength(2)
  })

  it('eyes blink only where they are drawn as eyes', () => {
    expect(render(<Vitya state="hello" />).container.querySelector('.vitya__eyes')).not.toBeNull()
    expect(render(<Vitya state="done" />).container.querySelector('.vitya__eyes')).toBeNull()
  })

  it('styles animate each state and switch off with prefers-reduced-motion', () => {
    expect(css).toMatch(/\.vitya--done \.vitya__tail \{ animation: vitya-wag/)
    expect(css).toMatch(/\.vitya--thinking \.vitya__ear--l \{ animation:/)
    expect(css).toMatch(/\.vitya--oops \.vitya__ear--l \{ transform: rotate/)
    expect(css).toMatch(/\.vitya--hello \.vitya__eyes \{ animation: vitya-blink/)
    const reduced = css.slice(css.lastIndexOf('@media (prefers-reduced-motion: reduce)'))
    expect(reduced).toMatch(/\.vitya, \.vitya \* \{ animation: none !important; transition: none !important; \}/)
  })
})

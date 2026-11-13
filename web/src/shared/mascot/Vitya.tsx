// «Авиитик Витя» — щенок-стажёр (Авиитик — от «ИИ», коротко Витя). 5 состояний (09 §10.2).
// Оригинальный рисунок; цвета — из семантических токенов темы.
// Живые состояния (09 §17.7, AG-33) — только CSS-классы в base.css: done — хвост
// машет, thinking — уши шевелятся, oops — уши опущены, hello — моргает;
// при prefers-reduced-motion анимаций нет.

export type VityaState = 'hello' | 'thinking' | 'done' | 'oops' | 'ask_mentor'

const TITLES: Record<VityaState, string> = {
  hello: 'Витя машет лапой',
  thinking: 'Витя думает',
  done: 'Витя радуется: готово',
  oops: 'Витя расстроен: что-то не получилось',
  ask_mentor: 'Витя предлагает спросить наставника',
}

function Eyes({ state }: { state: VityaState }) {
  if (state === 'done') {
    return (
      <g className="vitya__line" fill="none" strokeWidth="3" strokeLinecap="round">
        <path d="M43 58 q5 -6 10 0" />
        <path d="M67 58 q5 -6 10 0" />
      </g>
    )
  }
  const dx = state === 'thinking' ? 2 : 0
  const dy = state === 'thinking' ? -3 : 0
  return (
    <g className="vitya__eyes">
      <ellipse cx="48" cy="58" rx="5" ry="6" className="vitya__ink" />
      <ellipse cx="72" cy="58" rx="5" ry="6" className="vitya__ink" />
      <circle cx={49.5 + dx} cy={56 + dy} r="1.8" fill="#fff" />
      <circle cx={73.5 + dx} cy={56 + dy} r="1.8" fill="#fff" />
      {state === 'oops' && (
        <g className="vitya__line" fill="none" strokeWidth="2.5" strokeLinecap="round">
          <path d="M41 47 l10 3" />
          <path d="M79 47 l-10 3" />
        </g>
      )}
    </g>
  )
}

function Mouth({ state }: { state: VityaState }) {
  switch (state) {
    case 'done':
      return <path d="M50 74 q10 12 20 0 z" className="vitya__mouth" />
    case 'oops':
      return <ellipse cx="60" cy="77" rx="4" ry="3.5" className="vitya__mouth" />
    case 'thinking':
      return <path d="M55 76 h10" className="vitya__line" strokeWidth="2.5" strokeLinecap="round" />
    default:
      return <path d="M52 73 q8 8 16 0" className="vitya__line" fill="none" strokeWidth="2.5" strokeLinecap="round" />
  }
}

function Extra({ state }: { state: VityaState }) {
  switch (state) {
    case 'hello':
      return <path d="M98 70 q8 -10 4 -22 q-2 -5 -6 -2 q-3 8 -8 14 z" className="vitya__fur" />
    case 'thinking':
      return (
        <g className="vitya__accent vitya__dots">
          <circle cx="92" cy="22" r="3" />
          <circle cx="101" cy="16" r="3.5" />
          <circle cx="111" cy="10" r="4" />
        </g>
      )
    case 'done':
      return (
        <g>
          <circle cx="98" cy="22" r="11" className="vitya__ok" />
          <path d="M92 22 l4 4 l8 -8" stroke="#fff" strokeWidth="3" fill="none" strokeLinecap="round" />
        </g>
      )
    case 'oops':
      return <path d="M92 38 q4 7 0 10 q-4 -3 0 -10 z" className="vitya__drop" />
    case 'ask_mentor':
      return (
        <g>
          <circle cx="98" cy="22" r="12" className="vitya__accent" />
          <text x="98" y="28" textAnchor="middle" fontSize="16" fontWeight="700" fill="#fff">
            ?
          </text>
        </g>
      )
  }
}

export function Vitya({ state, size = 72 }: { state: VityaState; size?: number }) {
  const tilt = state === 'ask_mentor' ? 'rotate(-6 60 64)' : undefined
  return (
    <svg className={`vitya vitya--${state}`} width={size} height={size} viewBox="0 0 120 120" role="img" aria-label={TITLES[state]} data-state={state}>
      <title>{TITLES[state]}</title>
      {/* хвост — за головой, машет в done */}
      <path d="M22 104 q-14 -4 -14 -20 q0 -6 5 -5 q2 12 13 17 z" className="vitya__fur vitya__tail" />
      <g transform={tilt}>
        {/* уши */}
        <path d="M26 40 q-14 18 -6 40 q8 2 14 -6 q-2 -20 6 -30 z" className="vitya__ear vitya__ear--l" />
        <path d="M94 40 q14 18 6 40 q-8 2 -14 -6 q2 -20 -6 -30 z" className="vitya__ear vitya__ear--r" />
        {/* голова */}
        <ellipse cx="60" cy="62" rx="36" ry="33" className="vitya__fur" />
        <ellipse cx="60" cy="74" rx="18" ry="13" className="vitya__muzzle" />
        <ellipse cx="60" cy="68" rx="6" ry="4.5" className="vitya__ink" />
        <circle cx="38" cy="70" r="4.5" className="vitya__cheek" />
        <circle cx="82" cy="70" r="4.5" className="vitya__cheek" />
        <Eyes state={state} />
        <Mouth state={state} />
        {/* ошейник с жетоном «ИИ» */}
        <path d="M34 92 q26 12 52 0 l0 6 q-26 12 -52 0 z" className="vitya__accent" />
        <circle cx="60" cy="104" r="6" className="vitya__tag" />
        <text x="60" y="106.5" textAnchor="middle" fontSize="6.5" fontWeight="700" className="vitya__tag-text">
          ИИ
        </text>
      </g>
      <Extra state={state} />
    </svg>
  )
}

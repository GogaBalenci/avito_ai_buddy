// Синтетические фикстуры по сидам (11 §1.13): демо-стажёр Backend, неделя 1, день 4.
import type { AgentReply, AppConfig, LlmHealth, Me, PathView, RequestView, TestAccount, TimeOffConfig, MentorIntern, KbDoc, AccessResourceView, DemoFlag } from '@/shared/api/schemas'

export const config: AppConfig = { poll_interval_ms: 10000, demo_tools_enabled: true, timezone: 'Europe/Moscow', llm_provider: 'demo' }
export const llmHealth: LlmHealth = { provider: 'demo', model: null, status: 'disabled', last_error_code: null, checked_at: '2026-11-12T09:00:00Z' }

export const accounts: TestAccount[] = [
  { user_id: 'u-intern-be', display_name: 'Аня Смирнова', role: 'intern', description: 'Backend, неделя 1 — основной сценарий' },
  { user_id: 'u-intern-fe', display_name: 'Миша Котов', role: 'intern', description: 'Frontend (тестовый трек), неделя 2' },
  { user_id: 'u-mentor', display_name: 'Олег Наставников', role: 'mentor', description: 'Наставник обоих стажёров' },
]

export const meIntern: Me = {
  user_id: 'u-intern-be',
  display_name: 'Аня Смирнова',
  role: 'intern',
  track: { id: 'backend', title: 'Backend', synthetic: false, disclaimer: null },
}
export const meMentor: Me = { user_id: 'u-mentor', display_name: 'Олег Наставников', role: 'mentor', track: null }

export function path(overrides: Partial<PathView> = {}): PathView {
  return {
    employee: { user_id: 'u-intern-be', display_name: 'Аня Смирнова', track: { id: 'backend', title: 'Backend', synthetic: false, disclaimer: null } },
    today: '2026-11-12',
    start_date: '2026-11-09',
    day: 4,
    state: 'ready',
    current_period: 'week1',
    periods: [
      { id: 'week1', title: 'Неделя 1 — Вход в команду', from_day: 1, to_day: 7, state: 'current', done: 2, total: 6 },
      { id: 'week2', title: 'Неделя 2 — Инструменты и платформа', from_day: 8, to_day: 14, state: 'future', done: 0, total: 1 },
    ],
    steps: [
      { step_id: 'cm-1.6', checklist_ref: '1.6', period: 'week1', order: 10, title: 'Познакомься с командой', type: 'meet', help: { kind: 'explain', kb_doc: 'team-structure' }, status: 'done', synthetic: false, request: null },
      { step_id: 'cm-1.7', checklist_ref: '1.7', period: 'week1', order: 20, title: 'Попроси добавить тебя во все ключевые каналы и чаты', type: 'access', help: { kind: 'access_request', resource: 'team-channels' }, status: 'not_started', synthetic: false, request: null },
      { step_id: 'cm-1.10', checklist_ref: '1.10', period: 'week1', order: 30, title: 'Запроси доступ в пространство своего кластера и команды и изучи его', type: 'access', help: { kind: 'access_request', resource: 'space-cluster' }, status: 'waiting', synthetic: false, request: { request_id: 'ACC-000101', status: 'pending', auto_approve_at: null, reject_reason: null } },
      { step_id: 'cm-1.9', checklist_ref: '1.9', period: 'week1', order: 40, title: 'Заведи конспект встреч с наставником', type: 'org', help: { kind: 'track_only' }, status: 'not_started', synthetic: false, request: null },
      { step_id: 'be-1.11', checklist_ref: '1.11', period: 'week1', order: 50, title: 'Изучи принципы разработки', type: 'learn', help: { kind: 'explain', kb_doc: 'dev-principles' }, status: 'in_progress', synthetic: false, request: null },
      { step_id: 'be-1.12', checklist_ref: '1.12', period: 'week1', order: 60, title: 'Изучи «Авито в цифрах»', type: 'learn', help: { kind: 'explain', kb_doc: 'avito-in-numbers' }, status: 'done', synthetic: false, request: null },
      { step_id: 'cm-2.1', checklist_ref: '2.1', period: 'week2', order: 10, title: 'Сформулируй вопрос наставнику о плане', type: 'tell', help: { kind: 'ask_mentor' }, status: 'not_started', synthetic: false, request: null },
    ],
    next_step: { step_id: 'cm-1.7', reason: 'current' },
    waiting: ['cm-1.10'],
    progress: { period_done: 2, period_total: 6, overall_done: 2, overall_total: 7 },
    read_only: false,
    has_pending_requests: true,
    stale_requests: false,
    ...overrides,
  }
}

export const timeOffConfig: TimeOffConfig = { today: '2026-11-10', max_date: '2027-02-08', max_working_days: 5, horizon_days: 90, weekend_days: [6, 7] }

export function reply(overrides: Partial<AgentReply> = {}): AgentReply {
  return {
    reply_id: '00000000-0000-4000-8000-000000000001',
    client_message_id: '00000000-0000-4000-8000-0000000000aa',
    mode: 'demo',
    intent: 'help',
    mascot_state: 'hello',
    text: 'Привет! Я Витя, твой бадди на стажировке.',
    sources: [],
    actions: [],
    pending_action: null,
    escalation: null,
    notices: [],
    ...overrides,
  }
}

export const requestAbs: RequestView = {
  kind: 'time_off', request_id: 'ABS-000101', status: 'registered', created_at: '2026-11-10T09:00:00Z',
  date_from: '2026-11-30', date_to: '2026-12-01', working_days: 2,
}
export const requestAcc: RequestView = {
  kind: 'access', request_id: 'ACC-000101', status: 'pending', created_at: '2026-11-11T09:00:00Z',
  resource_id: 'space-cluster', resource_title: 'Пространство кластера', access_level: 'read', approver: 'Наставник', policy: 'mentor',
  step_id: 'cm-1.10', step_title: 'Запроси доступ в пространство своего кластера', justification: 'Шаг 1.10 чек-листа',
}

export const interns: MentorIntern[] = [
  { user_id: 'u-intern-be', display_name: 'Аня Смирнова', track: { id: 'backend', title: 'Backend', synthetic: false }, current_period: 'week1', day: 4, progress: { period_done: 2, period_total: 6, overall_done: 2, overall_total: 7 }, pending_access_requests: 1, upcoming_time_off: [{ request_id: 'ABS-000101', date_from: '2026-11-30', date_to: '2026-12-01' }] },
  { user_id: 'u-intern-fe', display_name: 'Миша Котов', track: { id: 'frontend', title: 'Frontend', synthetic: true, disclaimer: 'Тестовый трек' }, current_period: 'week2', day: 10, progress: { period_done: 1, period_total: 5, overall_done: 6, overall_total: 22 }, pending_access_requests: 0, upcoming_time_off: [] },
]

export const kbDoc: KbDoc = {
  slug: 'dev-principles', title: 'Принципы разработки', kind: 'guide', summary: 'Кратко о принципах.',
  body_md: '# Принципы\n\nПиши тесты. [Ссылка](https://example.com) и [плохая](javascript:alert(1)).\n\n<script>alert(1)</script>',
  label: 'test_instruction', source_level: 'playbook', dated: 'по мотивам Playbook 2020–2021', url: '/kb/dev-principles',
  updated_at: '2026-11-01T00:00:00Z', conflicts_with: [],
}

export const resources: AccessResourceView[] = [
  { resource_id: 'channel-tech-dep', title: 'Канал Tech-dep', kind: 'channel', description: 'Новости платформы', access_level: 'read', approver: null, effective_policy: 'timer', timer_minutes: 2 },
]

export const flags: DemoFlag[] = [
  { name: 'mock_absences_fault', value: 'off', allowed_values: ['off', 'unavailable'] },
  { name: 'access_mentor_approval', value: 'on', allowed_values: ['on', 'off'] },
]

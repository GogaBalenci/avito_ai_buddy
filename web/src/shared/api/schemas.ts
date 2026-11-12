// Zod-схемы ответов API. Каждая связана с типом из OpenAPI через `satisfies`:
// если поле в api/openapi.yaml изменилось, а схема — нет, падает `tsc` (12 §10.1).
import { z } from 'zod'
import type { components } from './schema.gen'

type S = components['schemas']

export const UserIdSchema = z.string()
export const RoleSchema = z.enum(['intern', 'mentor']) satisfies z.ZodType<S['Role']>
export const MascotStateSchema = z.enum(['hello', 'done', 'oops', 'ask_mentor']) satisfies z.ZodType<S['MascotState']>
export const StepStatusSchema = z.enum(['not_started', 'in_progress', 'waiting', 'done']) satisfies z.ZodType<S['StepStatus']>
export const PeriodIdSchema = z.enum(['week1', 'week2', 'month1', 'month2', 'month3', 'after3']) satisfies z.ZodType<S['PeriodId']>
export const RequestKindSchema = z.enum(['time_off', 'access']) satisfies z.ZodType<S['RequestKind']>
export const AccessLevelSchema = z.enum(['read', 'write', 'admin']) satisfies z.ZodType<S['AccessLevel']>
export const PolicySchema = z.enum(['auto', 'mentor', 'timer']) satisfies z.ZodType<S['Policy']>

export const HealthSchema = z.object({
  status: z.enum(['ok', 'degraded']),
  postgres: z.boolean(),
  redis: z.boolean(),
}) satisfies z.ZodType<S['Health']>

export const AppConfigSchema = z.object({
  poll_interval_ms: z.number().int(),
  demo_tools_enabled: z.boolean(),
  timezone: z.string(),
  llm_provider: z.enum(['demo', 'openai_compatible']),
}) satisfies z.ZodType<S['AppConfig']>

export const LlmHealthSchema = z.object({
  provider: z.enum(['demo', 'openai_compatible']),
  model: z.string().nullish(),
  status: z.enum(['ok', 'degraded', 'auth_error', 'misconfigured', 'circuit_open', 'disabled']),
  last_error_code: z.string().nullish(),
  checked_at: z.string(),
}) satisfies z.ZodType<S['LlmHealth']>

export const TrackRefSchema = z.object({
  id: z.string(),
  title: z.string(),
  synthetic: z.boolean(),
  disclaimer: z.string().nullish(),
}) satisfies z.ZodType<S['TrackRef']>

export const MeSchema = z.object({
  user_id: UserIdSchema,
  display_name: z.string(),
  role: RoleSchema,
  track: TrackRefSchema.nullish(),
}) satisfies z.ZodType<S['Me']>

export const TestAccountSchema = z.object({
  user_id: UserIdSchema,
  display_name: z.string(),
  role: RoleSchema,
  description: z.string(),
}) satisfies z.ZodType<S['TestAccount']>

export const TestAccountListSchema = z.object({ items: z.array(TestAccountSchema) })

export const SourceSchema = z.object({
  n: z.number().int(),
  slug: z.string(),
  title: z.string(),
  url: z.string(),
  label: z.enum(['test_instruction', 'test_description']),
  source_level: z.enum(['playbook', 'case', 'public', 'team', 'none']),
  dated: z.string().nullish(),
}) satisfies z.ZodType<S['Source']>

export const DateRangeSchema = z.object({ date_from: z.string(), date_to: z.string() }) satisfies z.ZodType<S['DateRange']>

export const TimeOffBriefSchema = z.object({
  request_id: z.string(),
  date_from: z.string(),
  date_to: z.string(),
}) satisfies z.ZodType<S['TimeOffBrief']>

export const ReplyActionSchema = z.object({
  type: z.enum(['quick_reply', 'open_form', 'confirm', 'cancel', 'edit', 'mark_done', 'request_access', 'copy', 'link', 'retry', 'feedback']),
  label: z.string(),
  payload: z.string().optional(),
  value: z.enum(['up', 'down']).optional(),
  action_id: z.string().optional(),
  step_id: z.string().optional(),
  form: z.enum(['time_off']).optional(),
  prefill: DateRangeSchema.nullish(),
  existing: z.array(TimeOffBriefSchema).optional(),
  text: z.string().optional(),
  copy_event: z.enum(['escalation_copied', 'doc_conflict_reported']).optional(),
  url: z.string().optional(),
}) satisfies z.ZodType<S['ReplyAction']>

export const ActionSummarySchema = z.object({
  date_from: z.string().optional(),
  date_to: z.string().optional(),
  working_days: z.number().int().optional(),
  resource_id: z.string().optional(),
  title: z.string().optional(),
  access_level: AccessLevelSchema.optional(),
  approver: z.string().nullish(),
  effective_policy: PolicySchema.optional(),
  timer_minutes: z.number().int().nullish(),
  step_id: z.string().nullish(),
  justification: z.string().optional(),
}) satisfies z.ZodType<S['ActionSummary']>

const ActionStateSchema = z.enum(['active', 'confirming', 'confirmed', 'cancelled', 'replaced', 'expired'])

export const PendingActionSchema = z.object({
  action_id: z.string(),
  kind: RequestKindSchema,
  state: ActionStateSchema,
  expires_at: z.string(),
  summary: ActionSummarySchema,
}) satisfies z.ZodType<S['PendingAction']>

export const EscalationSchema = z.object({
  reason: z.enum(['explicit', 'no_answer', 'no_source_term', 'permission_denied', 'not_understood', 'negative_feedback', 'mentor_topic']),
  draft: z.string(),
}) satisfies z.ZodType<S['Escalation']>

export const NoticeSchema = z.enum([
  'simplified_mode', 'llm_auth_error', 'llm_misconfigured', 'synthetic_track',
  'pii_masked', 'restricted_doc_exists', 'doc_conflict', 'overlap_unchecked',
])

export const AgentReplySchema = z.object({
  reply_id: z.string(),
  client_message_id: z.string(),
  mode: z.enum(['demo', 'llm', 'fallback']),
  intent: z.enum(['help', 'my_path', 'step_explain', 'step_done', 'kb_question', 'time_off', 'access_request', 'my_requests', 'ask_mentor', 'out_of_scope', 'unknown']),
  mascot_state: MascotStateSchema,
  text: z.string(),
  sources: z.array(SourceSchema),
  actions: z.array(ReplyActionSchema),
  pending_action: PendingActionSchema.nullish(),
  escalation: EscalationSchema.nullish(),
  notices: z.array(NoticeSchema),
}) satisfies z.ZodType<S['AgentReply']>

export const HistoryItemSchema = z.object({
  id: z.string(),
  ts: z.string(),
  author: z.enum(['user', 'bot']),
  text: z.string().nullish(),
  button_label: z.string().nullish(),
  reply: AgentReplySchema.nullish(),
}) satisfies z.ZodType<S['HistoryItem']>

export const HistoryPageSchema = z.object({ items: z.array(HistoryItemSchema) }) satisfies z.ZodType<S['HistoryPage']>

export const RequestViewSchema = z.object({
  kind: RequestKindSchema,
  request_id: z.string(),
  status: z.enum(['registered', 'cancelled', 'pending', 'approved', 'rejected']),
  created_at: z.string(),
  date_from: z.string().optional(),
  date_to: z.string().optional(),
  working_days: z.number().int().optional(),
  resource_id: z.string().optional(),
  resource_title: z.string().optional(),
  access_level: AccessLevelSchema.optional(),
  approver: z.string().nullish(),
  justification: z.string().nullish(),
  policy: PolicySchema.optional(),
  step_id: z.string().nullish(),
  step_title: z.string().nullish(),
  auto_approve_at: z.string().nullish(),
  decided_by: z.enum(['auto', 'timer', 'mentor']).nullish(),
  reject_reason: z.enum(['not_needed', 'other_resource', 'discuss_1on1']).nullish(),
  employee: z.object({ user_id: UserIdSchema, display_name: z.string() }).nullish(),
}) satisfies z.ZodType<S['RequestView']>

export const RequestListSchema = z.object({
  items: z.array(RequestViewSchema),
  total: z.number().int(),
  stale: z.boolean().optional(),
}) satisfies z.ZodType<S['RequestList']>

export const ActionCreatedSchema = z.object({
  pending_action: PendingActionSchema,
  notices: z.array(z.enum(['overlap_unchecked'])),
}) satisfies z.ZodType<S['ActionCreated']>

export const ActionViewSchema = z.object({
  action_id: z.string(),
  kind: RequestKindSchema,
  state: ActionStateSchema,
  expires_at: z.string(),
  summary: ActionSummarySchema,
  result: RequestViewSchema.nullish(),
}) satisfies z.ZodType<S['ActionView']>

export const ConfirmResponseSchema = z.object({
  result: RequestViewSchema,
  reply: AgentReplySchema,
}) satisfies z.ZodType<S['ConfirmResponse']>

export const PeriodSchema = z.object({
  id: PeriodIdSchema,
  title: z.string(),
  from_day: z.number().int(),
  to_day: z.number().int().nullish(),
  state: z.enum(['past', 'current', 'future']),
  done: z.number().int(),
  total: z.number().int(),
}) satisfies z.ZodType<S['Period']>

export const PathStepSchema = z.object({
  step_id: z.string(),
  checklist_ref: z.string().nullish(),
  period: PeriodIdSchema,
  order: z.number().int(),
  title: z.string(),
  type: z.enum(['learn', 'setup', 'access', 'meet', 'tell', 'org']),
  help: z.object({
    kind: z.enum(['explain', 'access_request', 'ask_mentor', 'track_only']),
    kb_doc: z.string().optional(),
    resource: z.string().optional(),
  }),
  status: StepStatusSchema,
  synthetic: z.boolean(),
  request: z.object({
    request_id: z.string(),
    status: z.enum(['pending', 'approved', 'rejected']),
    auto_approve_at: z.string().nullish(),
    reject_reason: z.string().nullish(),
  }).nullish(),
}) satisfies z.ZodType<S['PathStep']>

export const NextStepSchema = z.object({
  step_id: z.string(),
  reason: z.enum(['overdue', 'current', 'ahead']),
}) satisfies z.ZodType<S['NextStep']>

export const ProgressSchema = z.object({
  period_done: z.number().int(),
  period_total: z.number().int(),
  overall_done: z.number().int(),
  overall_total: z.number().int(),
}) satisfies z.ZodType<S['Progress']>

const PathStateSchema = z.enum(['ready', 'pre_start', 'only_waiting', 'finished'])

export const PathViewSchema = z.object({
  employee: z.object({ user_id: UserIdSchema, display_name: z.string(), track: TrackRefSchema }),
  today: z.string(),
  start_date: z.string(),
  day: z.number().int(),
  state: PathStateSchema,
  current_period: PeriodIdSchema,
  periods: z.array(PeriodSchema),
  steps: z.array(PathStepSchema),
  next_step: NextStepSchema.nullable(),
  waiting: z.array(z.string()),
  progress: ProgressSchema,
  read_only: z.boolean(),
  has_pending_requests: z.boolean(),
  stale_requests: z.boolean(),
}) satisfies z.ZodType<S['PathView']>

export const StepStatusResultSchema = z.object({
  step: PathStepSchema,
  changed: z.boolean(),
  path_summary: z.object({
    state: PathStateSchema,
    next_step: NextStepSchema.nullable(),
    waiting: z.array(z.string()),
    progress: ProgressSchema,
  }),
}) satisfies z.ZodType<S['StepStatusResult']>

export const TimeOffConfigSchema = z.object({
  today: z.string(),
  max_date: z.string(),
  max_working_days: z.number().int(),
  horizon_days: z.number().int(),
  weekend_days: z.array(z.number().int()),
}) satisfies z.ZodType<S['TimeOffConfig']>

export const AccessResourceViewSchema = z.object({
  resource_id: z.string(),
  title: z.string(),
  kind: z.enum(['channel', 'space', 'repo', 'tool', 'database']),
  description: z.string(),
  access_level: AccessLevelSchema,
  approver: z.string().nullish(),
  effective_policy: PolicySchema,
  timer_minutes: z.number().int().nullish(),
}) satisfies z.ZodType<S['AccessResourceView']>

export const KbDocSchema = z.object({
  slug: z.string(),
  title: z.string(),
  kind: z.enum(['process', 'term', 'tool', 'guide', 'org']),
  summary: z.string(),
  body_md: z.string(),
  label: z.enum(['test_instruction', 'test_description']),
  source_level: z.enum(['playbook', 'case', 'public', 'team', 'none']),
  dated: z.string().nullish(),
  url: z.string(),
  updated_at: z.string(),
  conflicts_with: z.array(z.string()),
}) satisfies z.ZodType<S['KbDoc']>

export const MentorInternSchema = z.object({
  user_id: UserIdSchema,
  display_name: z.string(),
  track: TrackRefSchema,
  current_period: PeriodIdSchema,
  day: z.number().int(),
  progress: ProgressSchema,
  pending_access_requests: z.number().int(),
  upcoming_time_off: z.array(TimeOffBriefSchema),
}) satisfies z.ZodType<S['MentorIntern']>

export const MentorInternListSchema = z.object({ items: z.array(MentorInternSchema) }) satisfies z.ZodType<S['MentorInternList']>

export const DemoFlagSchema = z.object({
  name: z.string(),
  value: z.string(),
  allowed_values: z.array(z.string()),
}) satisfies z.ZodType<S['DemoFlag']>

export const DemoFlagListSchema = z.object({ items: z.array(DemoFlagSchema) }) satisfies z.ZodType<S['DemoFlagList']>

export const FieldErrorSchema = z.object({
  field: z.enum(['date_from', 'date_to', 'range', 'resource_id', 'text', 'payload']),
  code: z.string(),
  message: z.string(),
  params: z.record(z.string(), z.unknown()).optional(),
}) satisfies z.ZodType<S['FieldError']>

export const ErrorBodySchema = z.object({
  code: z.string(),
  message: z.string(),
  retryable: z.boolean().optional(),
  errors: z.array(FieldErrorSchema).optional(),
  current_status: StepStatusSchema.optional(),
  existing: RequestViewSchema.optional(),
  request: RequestViewSchema.optional(),
  title: z.string().optional(),
  reply: AgentReplySchema.optional(),
}) satisfies z.ZodType<S['Error']>

export type AppConfig = z.infer<typeof AppConfigSchema>
export type LlmHealth = z.infer<typeof LlmHealthSchema>
export type Me = z.infer<typeof MeSchema>
export type TestAccount = z.infer<typeof TestAccountSchema>
export type Source = z.infer<typeof SourceSchema>
export type ReplyAction = z.infer<typeof ReplyActionSchema>
export type PendingAction = z.infer<typeof PendingActionSchema>
export type ActionSummary = z.infer<typeof ActionSummarySchema>
export type AgentReply = z.infer<typeof AgentReplySchema>
export type Notice = z.infer<typeof NoticeSchema>
export type HistoryItem = z.infer<typeof HistoryItemSchema>
export type RequestView = z.infer<typeof RequestViewSchema>
export type RequestList = z.infer<typeof RequestListSchema>
export type ActionView = z.infer<typeof ActionViewSchema>
export type ConfirmResponse = z.infer<typeof ConfirmResponseSchema>
export type PathView = z.infer<typeof PathViewSchema>
export type PathStep = z.infer<typeof PathStepSchema>
export type Period = z.infer<typeof PeriodSchema>
export type StepStatus = z.infer<typeof StepStatusSchema>
export type StepStatusResult = z.infer<typeof StepStatusResultSchema>
export type TimeOffConfig = z.infer<typeof TimeOffConfigSchema>
export type AccessResourceView = z.infer<typeof AccessResourceViewSchema>
export type KbDoc = z.infer<typeof KbDocSchema>
export type MentorIntern = z.infer<typeof MentorInternSchema>
export type DemoFlag = z.infer<typeof DemoFlagSchema>
export type FieldError = z.infer<typeof FieldErrorSchema>
export type ErrorBody = z.infer<typeof ErrorBodySchema>
export type MascotState = z.infer<typeof MascotStateSchema>

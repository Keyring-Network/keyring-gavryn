import { useEffect, useMemo, useState } from "react";
import { Bell, CalendarClock, Loader2, Play, Save, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || "http://localhost:8080";

type AutomationSummary = {
  id: string;
  name: string;
  prompt: string;
  model: string;
  days: string[];
  time: string;
  timezone: string;
  enabled: boolean;
  next_run_at?: string;
  last_run_at?: string;
  in_progress?: boolean;
  unread_count?: number;
  last_status?: string;
  created_at: string;
  updated_at: string;
};

type AutomationInboxEntry = {
  id: string;
  automation_id: string;
  run_id?: string;
  status: string;
  phase?: string;
  completion_reason?: string;
  final_response?: string;
  timed_out?: boolean;
  error?: string;
  unread: boolean;
  trigger: string;
  started_at: string;
  completed_at?: string;
};

type AutomationsListResponse = {
  automations: AutomationSummary[];
  unread_count: number;
};

type AutomationInboxResponse = {
  automation: AutomationSummary;
  inbox: AutomationInboxEntry[];
  unread_count: number;
};

type AutomationFormState = {
  id: string;
  name: string;
  prompt: string;
  model: string;
  days: string[];
  time: string;
  timezone: string;
  enabled: boolean;
};

const WEEK_DAYS = [
  { id: "sun", label: "Sun" },
  { id: "mon", label: "Mon" },
  { id: "tue", label: "Tue" },
  { id: "wed", label: "Wed" },
  { id: "thu", label: "Thu" },
  { id: "fri", label: "Fri" },
  { id: "sat", label: "Sat" },
];

function nowTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

function formatDateTime(value?: string): string {
  if (!value) return "-";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(parsed);
}

function createDefaultForm(defaultModel: string): AutomationFormState {
  return {
    id: "",
    name: "",
    prompt: "",
    model: defaultModel,
    days: ["mon", "tue", "wed", "thu", "fri"],
    time: "09:00",
    timezone: nowTimezone(),
    enabled: true,
  };
}

interface AutomationsPanelProps {
  defaultModel: string;
  modelOptions: string[];
  onUnreadCountChange?: (count: number) => void;
}

export function AutomationsPanel({ defaultModel, modelOptions, onUnreadCountChange }: AutomationsPanelProps) {
  const [automations, setAutomations] = useState<AutomationSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [inboxEntries, setInboxEntries] = useState<AutomationInboxEntry[]>([]);
  const [inboxLoading, setInboxLoading] = useState(false);
  const [form, setForm] = useState<AutomationFormState>(() => createDefaultForm(defaultModel));
  const [saving, setSaving] = useState(false);
  const [runningNow, setRunningNow] = useState(false);

  const availableModels = useMemo(() => {
    const set = new Set<string>();
    const values = [...modelOptions, defaultModel].map((value) => String(value || "").trim()).filter(Boolean);
    for (const value of values) {
      set.add(value);
    }
    return Array.from(set);
  }, [defaultModel, modelOptions]);

  const selectedAutomation = useMemo(
    () => automations.find((automation) => automation.id === selectedId) || null,
    [automations, selectedId]
  );

  const loadAutomations = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/automations`);
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to load automations");
      }
      const payload = (await response.json()) as Partial<AutomationsListResponse>;
      const items = Array.isArray(payload.automations) ? payload.automations : [];
      setAutomations(items);
      onUnreadCountChange?.(typeof payload.unread_count === "number" ? payload.unread_count : 0);
      setSelectedId((prev) => {
        if (prev && items.some((item) => item.id === prev)) return prev;
        return items[0]?.id || null;
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load automations");
    } finally {
      setLoading(false);
    }
  };

  const loadInbox = async (automationId: string) => {
    setInboxLoading(true);
    try {
      const response = await fetch(`${API_BASE_URL}/automations/${automationId}/inbox`);
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to load inbox");
      }
      const payload = (await response.json()) as Partial<AutomationInboxResponse>;
      const entries = Array.isArray(payload.inbox) ? payload.inbox : [];
      setInboxEntries(entries);
      if (typeof payload.unread_count === "number") {
        const totalUnread = automations.reduce((acc, automation) => {
          if (automation.id === automationId) return acc + payload.unread_count!;
          return acc + (automation.unread_count || 0);
        }, 0);
        onUnreadCountChange?.(totalUnread);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load inbox");
    } finally {
      setInboxLoading(false);
    }
  };

  const triggerDueProcessing = async () => {
    try {
      await fetch(`${API_BASE_URL}/automations/process-due`, { method: "POST" });
    } catch {
      // no-op
    }
  };

  useEffect(() => {
    void loadAutomations();
  }, []);

  useEffect(() => {
    if (!selectedId) {
      setInboxEntries([]);
      return;
    }
    void loadInbox(selectedId);
  }, [selectedId]);

  useEffect(() => {
    const interval = window.setInterval(() => {
      void triggerDueProcessing();
      void loadAutomations();
      if (selectedId) {
        void loadInbox(selectedId);
      }
    }, 60000);
    return () => window.clearInterval(interval);
  }, [selectedId]);

  const resetForm = () => setForm(createDefaultForm(defaultModel));

  const applyAutomationToForm = (automation: AutomationSummary) => {
    setForm({
      id: automation.id,
      name: automation.name,
      prompt: automation.prompt,
      model: automation.model || defaultModel,
      days: Array.isArray(automation.days) && automation.days.length > 0 ? automation.days : ["mon", "tue", "wed", "thu", "fri"],
      time: automation.time || "09:00",
      timezone: automation.timezone || nowTimezone(),
      enabled: Boolean(automation.enabled),
    });
  };

  const toggleDay = (day: string) => {
    setForm((prev) => {
      const selected = prev.days.includes(day);
      const nextDays = selected ? prev.days.filter((entry) => entry !== day) : [...prev.days, day];
      return { ...prev, days: nextDays };
    });
  };

  const saveAutomation = async () => {
    const name = form.name.trim();
    const prompt = form.prompt.trim();
    if (!name || !prompt) {
      setError("Automation name and prompt are required.");
      return;
    }
    if (!form.time) {
      setError("Select a run time.");
      return;
    }

    setSaving(true);
    setError(null);
    const payload = {
      name,
      prompt,
      model: form.model.trim(),
      days: form.days,
      time: form.time,
      timezone: form.timezone,
      enabled: form.enabled,
    };

    try {
      const endpoint = form.id ? `${API_BASE_URL}/automations/${form.id}` : `${API_BASE_URL}/automations`;
      const method = form.id ? "PUT" : "POST";
      const response = await fetch(endpoint, {
        method,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to save automation");
      }
      resetForm();
      await loadAutomations();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save automation");
    } finally {
      setSaving(false);
    }
  };

  const deleteAutomation = async () => {
    if (!form.id) return;
    setSaving(true);
    setError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/automations/${form.id}`, { method: "DELETE" });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to delete automation");
      }
      if (selectedId === form.id) {
        setSelectedId(null);
        setInboxEntries([]);
      }
      resetForm();
      await loadAutomations();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete automation");
    } finally {
      setSaving(false);
    }
  };

  const runNow = async () => {
    if (!selectedId) return;
    setRunningNow(true);
    setError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/automations/${selectedId}/run`, { method: "POST" });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to queue automation run");
      }
      await loadAutomations();
      await loadInbox(selectedId);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to queue automation run");
    } finally {
      setRunningNow(false);
    }
  };

  const markRead = async (entryId: string) => {
    if (!selectedId) return;
    try {
      await fetch(`${API_BASE_URL}/automations/${selectedId}/inbox/${entryId}/read`, { method: "POST" });
      await loadAutomations();
      await loadInbox(selectedId);
    } catch {
      // no-op
    }
  };

  const markAllRead = async () => {
    if (!selectedId) return;
    try {
      await fetch(`${API_BASE_URL}/automations/${selectedId}/inbox/read-all`, { method: "POST" });
      await loadAutomations();
      await loadInbox(selectedId);
    } catch {
      // no-op
    }
  };

  return (
    <section className="h-full overflow-auto grid gap-6 xl:grid-cols-[minmax(0,1fr)_420px]">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <CalendarClock className="h-4 w-4" />
            Automations
          </CardTitle>
          <p className="text-sm text-muted-foreground">
            Schedule recurring tasks by day/time with model and prompt configuration.
          </p>
        </CardHeader>
        <CardContent className="space-y-5">
          <div className="grid gap-3 md:grid-cols-2">
            <div className="space-y-2 md:col-span-2">
              <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Name</div>
              <input
                value={form.name}
                onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="Daily DeFi brief"
                className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
              />
            </div>

            <div className="space-y-2">
              <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Time</div>
              <input
                type="time"
                value={form.time}
                onChange={(event) => setForm((prev) => ({ ...prev, time: event.target.value }))}
                className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
              />
            </div>

            <div className="space-y-2">
              <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Timezone</div>
              <input
                value={form.timezone}
                onChange={(event) => setForm((prev) => ({ ...prev, timezone: event.target.value }))}
                placeholder="America/New_York"
                className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
              />
            </div>

            <div className="space-y-2 md:col-span-2">
              <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Model</div>
              {availableModels.length > 0 ? (
                <select
                  value={form.model}
                  onChange={(event) => setForm((prev) => ({ ...prev, model: event.target.value }))}
                  className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                >
                  {availableModels.map((model) => (
                    <option key={model} value={model}>
                      {model}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  value={form.model}
                  onChange={(event) => setForm((prev) => ({ ...prev, model: event.target.value }))}
                  placeholder="gpt-5.2-codex"
                  className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                />
              )}
            </div>

            <div className="space-y-2 md:col-span-2">
              <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Days</div>
              <div className="flex flex-wrap gap-2">
                {WEEK_DAYS.map((day) => {
                  const selected = form.days.includes(day.id);
                  return (
                    <button
                      key={day.id}
                      type="button"
                      onClick={() => toggleDay(day.id)}
                      className={cn(
                        "rounded-lg border px-2.5 py-1 text-xs transition",
                        selected
                          ? "border-emerald-400/50 bg-emerald-500/10 text-emerald-200"
                          : "border-border/60 bg-card/40 text-muted-foreground hover:border-border"
                      )}
                    >
                      {day.label}
                    </button>
                  );
                })}
              </div>
            </div>

            <div className="space-y-2 md:col-span-2">
              <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Prompt</div>
              <textarea
                value={form.prompt}
                onChange={(event) => setForm((prev) => ({ ...prev, prompt: event.target.value }))}
                placeholder="Browse the web and send me a daily RWA market briefing with sources."
                className="min-h-[120px] w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
              />
            </div>

            <label className="md:col-span-2 flex items-center gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(event) => setForm((prev) => ({ ...prev, enabled: event.target.checked }))}
              />
              Enabled
            </label>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button onClick={() => void saveAutomation()} disabled={saving}>
              {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
              {form.id ? "Update automation" : "Create automation"}
            </Button>
            {form.id ? (
              <>
                <Button variant="outline" onClick={resetForm} disabled={saving}>New automation</Button>
                <Button variant="destructive" onClick={() => void deleteAutomation()} disabled={saving}>
                  <Trash2 className="mr-2 h-4 w-4" />
                  Delete
                </Button>
              </>
            ) : null}
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs uppercase tracking-[0.2em] text-muted-foreground">
              <span>Existing automations</span>
              <span>{automations.length}</span>
            </div>
            {loading ? (
              <div className="text-sm text-muted-foreground">Loading automations...</div>
            ) : automations.length === 0 ? (
              <div className="rounded-xl border border-dashed border-border/60 bg-muted/30 p-4 text-sm text-muted-foreground">
                No automations configured yet.
              </div>
            ) : (
              <div className="space-y-2">
                {automations.map((automation) => (
                  <button
                    key={automation.id}
                    type="button"
                    onClick={() => {
                      setSelectedId(automation.id);
                      applyAutomationToForm(automation);
                    }}
                    className={cn(
                      "w-full rounded-xl border px-3 py-2 text-left transition",
                      selectedId === automation.id
                        ? "border-accent/70 bg-accent/10"
                        : "border-border/60 bg-card/40 hover:border-border"
                    )}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <div className="truncate text-sm font-semibold text-foreground">{automation.name}</div>
                      <div className="flex items-center gap-2">
                        {automation.unread_count ? (
                          <span className="rounded-full bg-rose-500/15 px-2 py-0.5 text-[10px] font-semibold text-rose-300">
                            {automation.unread_count}
                          </span>
                        ) : null}
                        {automation.in_progress ? <Loader2 className="h-3.5 w-3.5 animate-spin text-orange-400" /> : null}
                      </div>
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                      {automation.enabled ? "Enabled" : "Disabled"} · Next run {formatDateTime(automation.next_run_at)}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>

          {error ? (
            <div className="rounded-xl border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-sm text-rose-100">{error}</div>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="flex items-center gap-2">
            <Bell className="h-4 w-4" />
            Automation inbox
          </CardTitle>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => void runNow()} disabled={!selectedId || runningNow}>
              {runningNow ? <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> : <Play className="mr-2 h-3.5 w-3.5" />}Run now
            </Button>
            <Button variant="ghost" size="sm" onClick={() => void markAllRead()} disabled={!selectedId}>Mark all read</Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          {!selectedAutomation ? (
            <div className="rounded-xl border border-dashed border-border/60 bg-muted/30 p-4 text-sm text-muted-foreground">
              Select an automation to view next run and inbox history.
            </div>
          ) : (
            <>
              <div className="rounded-xl border border-border/60 bg-card/40 p-3 text-sm">
                <div className="font-semibold text-foreground">{selectedAutomation.name}</div>
                <div className="mt-1 text-xs text-muted-foreground">Next run: {formatDateTime(selectedAutomation.next_run_at)}</div>
                <div className="text-xs text-muted-foreground">Last run: {formatDateTime(selectedAutomation.last_run_at)}</div>
              </div>

              {inboxLoading ? (
                <div className="text-sm text-muted-foreground">Loading inbox...</div>
              ) : inboxEntries.length === 0 ? (
                <div className="rounded-xl border border-dashed border-border/60 bg-muted/30 p-4 text-sm text-muted-foreground">
                  No automation runs yet.
                </div>
              ) : (
                <div className="max-h-[540px] space-y-2 overflow-auto pr-1">
                  {inboxEntries.map((entry) => (
                    <button
                      key={entry.id}
                      type="button"
                      onClick={() => {
                        if (entry.unread) {
                          void markRead(entry.id);
                        }
                      }}
                      className={cn(
                        "w-full rounded-xl border px-3 py-2 text-left transition",
                        entry.unread
                          ? "border-emerald-400/40 bg-emerald-500/5"
                          : "border-border/60 bg-card/30"
                      )}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <div className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                          {entry.status} · {entry.trigger}
                        </div>
                        {entry.unread ? <span className="h-2 w-2 rounded-full bg-emerald-400" /> : null}
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">Started {formatDateTime(entry.started_at)}</div>
                      <div className="mt-1 text-sm text-foreground line-clamp-5 whitespace-pre-wrap break-words">
                        {entry.final_response || entry.error || "No response captured."}
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </section>
  );
}

import type { ChatMessage } from "@/App";

export type ConversationMessageRole = "user" | "assistant";

export type ConversationItem =
  | {
      id: string;
      seq: number;
      kind: "message";
      role: ConversationMessageRole;
      text: string;
    }
  | {
      id: string;
      seq: number;
      kind: "reasoning";
      summary: string;
      content: string;
    }
  | {
      id: string;
      seq: number;
      kind: "tool";
      toolType: string;
      title: string;
      detail?: string;
      output?: string;
      status?: string;
      invocationId?: string;
      artifacts?: Array<{ uri: string; type?: string; contentType?: string }>;
    }
  | {
      id: string;
      seq: number;
      kind: "explore";
      status: "exploring" | "explored";
      invocationId?: string;
      entries: Array<{ kind: "read" | "search" | "list"; label: string; detail?: string }>;
    }
  | {
      id: string;
      seq: number;
      kind: "diff";
      title: string;
      status?: string;
      diff: string;
    }
  | {
      id: string;
      seq: number;
      kind: "review";
      state: "started" | "completed";
      text: string;
    };

export type ToolGroupItem = Exclude<ConversationItem, { kind: "message" }>;

export type MessageListEntry =
  | { kind: "item"; item: ConversationItem }
  | {
      kind: "toolGroup";
      group: {
        id: string;
        items: ToolGroupItem[];
        toolCount: number;
        messageCount: number;
        readsAndSearchesCount: number;
        filesChangedCount: number;
        commandsCount: number;
        screenshotsCount: number;
      };
    };

export type FeedBuildInput = {
  messages: ChatMessage[];
  events: Array<{
    seq: number;
    type: string;
    payload?: Record<string, any>;
  }>;
};

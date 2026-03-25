import { useRef, useEffect } from "react";
import type { ChatMessage as ChatMessageType } from "@/lib/types";
import ChatMessage from "./ChatMessage";
import { Bot } from "lucide-react";

interface ChatAreaProps {
  messages: ChatMessageType[];
  isRunning: boolean;
}

export default function ChatArea({ messages, isRunning }: ChatAreaProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  if (messages.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full select-none">
        <div className="flex items-center justify-center w-16 h-16 rounded-2xl bg-accent/8 mb-5">
          <Bot size={32} className="text-accent/60" />
        </div>
        <h2 className="text-lg font-semibold text-slate-300 mb-1.5">
          你好，有什么可以帮你？
        </h2>
        <p className="text-sm text-slate-500 max-w-sm text-center leading-relaxed">
          我可以帮你完成任务、分析文件、编写代码等。
          <br />
          请在下方输入你的需求。
        </p>
      </div>
    );
  }

  return (
    <div
      ref={scrollRef}
      className="h-full overflow-y-auto px-4 md:px-8 pt-10 pb-4"
    >
      <div className="max-w-3xl mx-auto space-y-1">
        {messages.map((msg) => (
          <ChatMessage key={msg.id} message={msg} />
        ))}

        {/* Typing indicator when running but no streaming */}
        {isRunning &&
          !messages.some((m) => m.streaming) &&
          messages[messages.length - 1]?.role !== "assistant" && (
            <div className="flex items-center gap-2 py-3 animate-fade-in">
              <div className="flex gap-1">
                <span className="w-1.5 h-1.5 rounded-full bg-accent/60 animate-bounce [animation-delay:0ms]" />
                <span className="w-1.5 h-1.5 rounded-full bg-accent/60 animate-bounce [animation-delay:150ms]" />
                <span className="w-1.5 h-1.5 rounded-full bg-accent/60 animate-bounce [animation-delay:300ms]" />
              </div>
              <span className="text-xs text-slate-500">思考中…</span>
            </div>
          )}

        <div ref={bottomRef} />
      </div>
    </div>
  );
}

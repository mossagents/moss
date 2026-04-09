import { useRef, useEffect } from "react";
import type { ChatMessage as ChatMessageType } from "@/lib/types.ts";
import ChatMessage from "./ChatMessage.tsx";

interface ChatAreaProps {
  messages: ChatMessageType[];
  isRunning: boolean;
}

export default function ChatArea({ messages, isRunning }: ChatAreaProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  if (messages.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full select-none">
        <div
          className="w-16 h-16 rounded-2xl bg-primary flex items-center justify-center mb-5 shadow-botanical-empty"
        >
          <span className="material-symbols-outlined text-on-primary text-3xl">auto_awesome</span>
        </div>
        <h2 className="text-xl font-bold text-on-surface mb-1.5 font-headline">
          你好，有什么可以帮你？
        </h2>
        <p className="text-sm text-on-surface-variant max-w-sm text-center leading-relaxed">
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
      className="h-full overflow-y-auto px-4 md:px-8 pt-8 pb-4"
    >
      <div className="max-w-4xl mx-auto space-y-12">
        {messages.map((msg) => (
          <ChatMessage key={msg.id} message={msg} />
        ))}

        {/* Typing indicator */}
        {isRunning &&
          !messages.some((m) => m.streaming) &&
          messages[messages.length - 1]?.role !== "assistant" && (
            <div className="flex gap-6 animate-fade-in">
              <div className="w-8 h-8 rounded-lg bg-primary flex items-center justify-center shrink-0 mt-1 shadow-sm">
                <span className="material-symbols-outlined text-on-primary text-sm">auto_awesome</span>
              </div>
              <div className="flex items-center gap-1.5 py-3">
                <span className="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce bounce-d0" />
                <span className="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce bounce-d150" />
                <span className="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce bounce-d300" />
              </div>
            </div>
          )}

        <div ref={bottomRef} />
      </div>
    </div>
  );
}

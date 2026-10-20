import React, { useState, useEffect, useRef } from 'react'
import {
  Terminal as TerminalIcon,
  Trash2,
  ArrowDown,
  Copy,
  Check,
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Clock,
} from 'lucide-react'
import type { LogEntry, BuildStatusSummary } from '../types'

interface TerminalLogsProps {
  logs: LogEntry[]
  onClear: () => void
  currentBuild: BuildStatusSummary | null
}

export const TerminalLogs: React.FC<TerminalLogsProps> = ({
  logs,
  onClear,
  currentBuild,
}) => {
  const [filter, setFilter] = useState<'all' | 'stdout' | 'stderr' | 'system'>('all')
  const [autoScroll, setAutoScroll] = useState(true)
  const [copied, setCopied] = useState(false)
  const terminalEndRef = useRef<HTMLDivElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (autoScroll && terminalEndRef.current) {
      terminalEndRef.current.scrollIntoView({ behavior: 'smooth' })
    }
  }, [logs, autoScroll])

  const copyAMI = (ami: string) => {
    navigator.clipboard.writeText(ami)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const filteredLogs = logs.filter((log) => {
    if (filter === 'all') return true
    return log.stream === filter
  })

  const formatTimestamp = (ts: string) => {
    try {
      const d = new Date(ts)
      return d.toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
    } catch {
      return ''
    }
  }

  return (
    <div className="flex flex-col h-full bg-black border-t border-zinc-800 text-xs font-mono">
      {/* Terminal Header Bar */}
      <div className="h-10 bg-zinc-950 border-b border-zinc-800 px-3 flex items-center justify-between shrink-0 select-none">
        <div className="flex items-center gap-2">
          <TerminalIcon className="w-3.5 h-3.5 text-zinc-400" />
          <span className="font-semibold text-zinc-200">Terminal Logs</span>
          <span className="text-[10px] text-zinc-500 font-mono">
            {logs.length} lines
          </span>
        </div>

        {/* Filters and Actions */}
        <div className="flex items-center gap-2">
          {/* Stream Filter */}
          <div className="flex items-center bg-zinc-900 rounded-md p-0.5 border border-zinc-800 text-[11px]">
            {(['all', 'system', 'stdout', 'stderr'] as const).map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setFilter(s)}
                className={`px-2 py-0.5 rounded capitalize transition-colors ${
                  filter === s
                    ? 'bg-white text-black font-medium'
                    : 'text-zinc-400 hover:text-white'
                }`}
              >
                {s}
              </button>
            ))}
          </div>

          {/* Auto Scroll Toggle */}
          <button
            type="button"
            onClick={() => setAutoScroll(!autoScroll)}
            className={`p-1.5 rounded-md border transition-colors ${
              autoScroll
                ? 'border-zinc-600 bg-zinc-800 text-white'
                : 'border-zinc-800 bg-zinc-900 text-zinc-500 hover:text-zinc-300'
            }`}
            title={autoScroll ? 'Auto-scroll enabled' : 'Auto-scroll disabled'}
          >
            <ArrowDown className="w-3.5 h-3.5" />
          </button>

          {/* Clear Logs */}
          <button
            type="button"
            onClick={onClear}
            className="p-1.5 text-zinc-500 hover:text-white bg-zinc-900 border border-zinc-800 hover:border-zinc-700 rounded-md transition-colors"
            title="Clear logs"
          >
            <Trash2 className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>

      {/* Final Outcome Banner (if build has finished) */}
      {currentBuild && currentBuild.status === 'success' && currentBuild.ami_id && (
        <div className="p-3 bg-zinc-900 border-b border-zinc-800 flex items-center justify-between gap-3 shrink-0">
          <div className="flex items-center gap-2.5">
            <div className="w-6 h-6 rounded-md bg-white text-black flex items-center justify-center shrink-0">
              <CheckCircle2 className="w-4 h-4 text-black stroke-[2.5]" />
            </div>
            <div>
              <div className="text-white font-medium text-xs font-sans">
                AMI Created Successfully
              </div>
              <div className="flex items-center gap-2 mt-0.5 font-mono">
                <span className="text-zinc-400 text-[11px] font-sans">AMI ID:</span>
                <code className="text-white bg-black border border-zinc-700 px-2 py-0.5 rounded text-xs font-bold">
                  {currentBuild.ami_id}
                </code>
              </div>
            </div>
          </div>

          <button
            type="button"
            onClick={() => copyAMI(currentBuild.ami_id!)}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-white text-black hover:bg-zinc-200 font-sans text-xs font-medium transition-colors"
          >
            {copied ? (
              <>
                <Check className="w-3.5 h-3.5" />
                Copied
              </>
            ) : (
              <>
                <Copy className="w-3.5 h-3.5" />
                Copy AMI ID
              </>
            )}
          </button>
        </div>
      )}

      {currentBuild && currentBuild.status === 'failed' && (
        <div className="p-3 bg-zinc-900 border-b border-zinc-800 flex items-center gap-2.5 shrink-0">
          <XCircle className="w-4 h-4 text-zinc-400 shrink-0" />
          <div>
            <div className="text-white font-medium text-xs font-sans">
              Packer Execution Failed
            </div>
            <div className="text-zinc-400 text-[11px] mt-0.5 font-sans">
              {currentBuild.error || 'Check log lines below for troubleshooting information.'}
            </div>
          </div>
        </div>
      )}

      {currentBuild && currentBuild.status === 'cancelled' && (
        <div className="p-3 bg-zinc-900 border-b border-zinc-800 flex items-center gap-2.5 shrink-0">
          <AlertTriangle className="w-4 h-4 text-zinc-400 shrink-0" />
          <div>
            <div className="text-zinc-200 font-medium text-xs font-sans">
              Build Cancelled by User
            </div>
            <div className="text-zinc-500 text-[11px] mt-0.5 font-sans">
              Packer sent SIGINT to cleanly terminate EC2 resources.
            </div>
          </div>
        </div>
      )}

      {/* Log Output Body */}
      <div
        ref={containerRef}
        className="flex-1 p-3 overflow-y-auto space-y-1 font-mono text-[11px] leading-relaxed select-text bg-black"
      >
        {filteredLogs.length === 0 ? (
          <div className="h-full flex flex-col items-center justify-center text-zinc-600 gap-1.5 select-none">
            <Clock className="w-5 h-5 opacity-40" />
            <p className="text-xs text-zinc-500 font-sans">No logs recorded yet</p>
            <p className="text-[11px] text-zinc-600 font-sans">
              Run &quot;Validate&quot; or &quot;Build AMI&quot; to see real-time output streamed from Packer.
            </p>
          </div>
        ) : (
          filteredLogs.map((entry, idx) => {
            const timeStr = formatTimestamp(entry.timestamp)

            let streamTag = '[INFO]'
            let tagColor = 'text-zinc-500'
            let textColor = 'text-zinc-300'

            if (entry.stream === 'system') {
              streamTag = '[STATUS]'
              tagColor = 'text-white'
              textColor = 'text-zinc-100 font-medium'
            } else if (entry.stream === 'stderr') {
              streamTag = '[STDERR]'
              tagColor = 'text-zinc-400 font-semibold'
              textColor = 'text-white'
            } else if (entry.message.toLowerCase().includes('error')) {
              textColor = 'text-white font-semibold'
            } else if (entry.message.includes('==>') || entry.message.includes('Build successful!')) {
              textColor = 'text-white font-medium'
            }

            return (
              <div key={idx} className="flex items-start gap-2 px-1 py-0.5 rounded hover:bg-zinc-950">
                {timeStr && (
                  <span className="text-zinc-600 shrink-0 select-none text-[10px]">
                    {timeStr}
                  </span>
                )}
                <span className={`text-[10px] font-mono shrink-0 select-none ${tagColor}`}>
                  {streamTag}
                </span>
                <span className={`flex-1 break-all whitespace-pre-wrap ${textColor}`}>
                  {entry.message}
                </span>
              </div>
            )
          })
        )}
        <div ref={terminalEndRef} />
      </div>
    </div>
  )
}

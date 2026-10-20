import React from 'react'
import Editor from '@monaco-editor/react'
import {
  FileCode2,
  CheckCircle,
  Download,
  Play,
  Square,
  RotateCcw,
  Loader2,
  SlidersHorizontal,
  AlertCircle,
  X,
  ShieldCheck,
} from 'lucide-react'
import type { BuildStatusSummary } from '../types'

interface HclEditorProps {
  hclCode: string
  onChange: (value: string) => void
  isManualMode: boolean
  onResetToForm: () => void
  onValidate: () => void
  isValidating: boolean
  onPreflight?: () => void
  isPreflighting?: boolean
  onBuild: () => void
  onCancel: () => void
  currentBuild: BuildStatusSummary | null
  onSyncToForm?: () => void
  isSyncingToForm?: boolean
  syncNotification?: { type: 'success' | 'error'; message: string } | null
  onClearNotification?: () => void
}

export const HclEditor: React.FC<HclEditorProps> = ({
  hclCode,
  onChange,
  isManualMode,
  onResetToForm,
  onValidate,
  isValidating,
  onPreflight,
  isPreflighting = false,
  onBuild,
  onCancel,
  currentBuild,
  onSyncToForm,
  isSyncingToForm = false,
  syncNotification = null,
  onClearNotification,
}) => {
  const isBuilding =
    currentBuild?.status === 'running' ||
    currentBuild?.status === 'pending' ||
    currentBuild?.status === 'cancelling'

  const handleDownload = () => {
    const blob = new Blob([hclCode], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'aws-build.pkr.hcl'
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  }

  const handleBeforeMount = (monaco: any) => {
    monaco.editor.defineTheme('vib-monochrome', {
      base: 'vs-dark',
      inherit: true,
      rules: [
        { token: '', foreground: 'd4d4d8' },
        { token: 'string', foreground: 'ffffff' },
        { token: 'keyword', foreground: 'ffffff', fontStyle: 'bold' },
        { token: 'comment', foreground: '71717a', fontStyle: 'italic' },
        { token: 'number', foreground: 'e4e4e7' },
        { token: 'type', foreground: 'fafafa' },
      ],
      colors: {
        'editor.background': '#000000',
        'editor.foreground': '#f4f4f5',
        'editor.lineHighlightBackground': '#18181b50',
        'editorLineNumber.foreground': '#52525b',
        'editorLineNumber.activeForeground': '#fafafa',
        'editorCursor.foreground': '#ffffff',
        'editor.selectionBackground': '#27272a',
        'editor.inactiveSelectionBackground': '#18181b',
      },
    })
  }

  return (
    <div className="flex flex-col h-full bg-black">
      {/* Editor Control Bar */}
      <div className="h-11 border-b border-zinc-800 bg-zinc-950 px-3 flex items-center justify-between shrink-0 select-none">
        {/* Left: Tab & Mode indicator */}
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-black border border-zinc-800 text-xs font-mono text-zinc-300">
            <FileCode2 className="w-3.5 h-3.5 text-zinc-400" />
            aws-build.pkr.hcl
          </div>

          {isManualMode ? (
            <div className="flex items-center gap-1.5">
              <span className="text-[11px] font-medium text-zinc-300 bg-zinc-900 border border-zinc-700 px-2 py-0.5 rounded-md flex items-center gap-1 font-mono">
                <span className="w-1.5 h-1.5 rounded-full bg-white"></span>
                Manual Edit
              </span>

              {onSyncToForm && (
                <button
                  type="button"
                  onClick={onSyncToForm}
                  disabled={isSyncingToForm || isBuilding}
                  className="text-[11px] font-medium text-black bg-white hover:bg-zinc-200 px-2.5 py-0.5 rounded-md flex items-center gap-1.5 transition-colors disabled:opacity-50"
                  title="Parse HCL into Visual Builder form"
                >
                  {isSyncingToForm ? (
                    <Loader2 className="w-3 h-3 animate-spin text-black" />
                  ) : (
                    <SlidersHorizontal className="w-3 h-3 text-black" />
                  )}
                  Sync to Form
                </button>
              )}

              <button
                type="button"
                onClick={onResetToForm}
                className="text-[11px] text-zinc-400 hover:text-white bg-zinc-900 hover:bg-zinc-800 px-2 py-0.5 rounded-md border border-zinc-800 flex items-center gap-1 transition-colors"
                title="Discard manual edits and sync with UI form"
              >
                <RotateCcw className="w-3 h-3" />
                Reset
              </button>
            </div>
          ) : (
            <span className="text-[11px] text-zinc-500 font-mono">
              Auto-Synced
            </span>
          )}
        </div>

        {/* Right: Actions */}
        <div className="flex items-center gap-2">
          {/* Pre-Flight Button */}
          {onPreflight && (
            <button
              type="button"
              onClick={onPreflight}
              disabled={isPreflighting || isBuilding}
              className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium border border-zinc-700 text-zinc-300 hover:border-zinc-400 hover:text-white bg-transparent disabled:opacity-50 transition-colors"
              title="Run AWS Pre-Flight readiness check (STS, Network, AMI, EC2 Permissions)"
            >
              {isPreflighting ? (
                <>
                  <Loader2 className="w-3.5 h-3.5 animate-spin text-zinc-400" />
                  Pre-Flight...
                </>
              ) : (
                <>
                  <ShieldCheck className="w-3.5 h-3.5 text-zinc-400" />
                  Pre-Flight
                </>
              )}
            </button>
          )}

          {/* Validate Button */}
          <button
            type="button"
            onClick={onValidate}
            disabled={isValidating || isBuilding}
            className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium border border-zinc-700 text-zinc-300 hover:border-zinc-400 hover:text-white bg-transparent disabled:opacity-50 transition-colors"
            title="Run packer validate on this template"
          >
            {isValidating ? (
              <>
                <Loader2 className="w-3.5 h-3.5 animate-spin text-zinc-400" />
                Validating...
              </>
            ) : (
              <>
                <CheckCircle className="w-3.5 h-3.5 text-zinc-400" />
                Validate
              </>
            )}
          </button>

          {/* Download Button */}
          <button
            type="button"
            onClick={handleDownload}
            className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs border border-zinc-800 bg-zinc-900 text-zinc-400 hover:border-zinc-700 hover:text-white transition-colors"
            title="Download aws-build.pkr.hcl"
          >
            <Download className="w-3.5 h-3.5" />
            <span className="hidden sm:inline">Export</span>
          </button>

          {/* Cancel or Build Button */}
          {isBuilding ? (
            <button
              type="button"
              onClick={onCancel}
              disabled={currentBuild?.status === 'cancelling'}
              className="flex items-center gap-1.5 px-3 py-1 rounded-md text-xs font-medium border border-zinc-700 bg-zinc-900 hover:bg-zinc-800 text-white disabled:opacity-50 transition-colors"
            >
              <Square className="w-3.5 h-3.5 fill-current" />
              {currentBuild?.status === 'cancelling' ? 'Cancelling...' : 'Cancel Build'}
            </button>
          ) : (
            <button
              type="button"
              onClick={onBuild}
              className="flex items-center gap-1.5 px-3.5 py-1 rounded-md text-xs bg-white text-black font-medium hover:bg-zinc-200 transition-colors"
            >
              <Play className="w-3.5 h-3.5 fill-current" />
              Build AMI
            </button>
          )}
        </div>
      </div>

      {/* Sync / Diagnostic Notification Banner */}
      {syncNotification && (
        <div
          className={`px-3 py-1.5 text-xs flex items-center justify-between border-b shrink-0 transition-colors ${
            syncNotification.type === 'success'
              ? 'bg-zinc-900 border-zinc-700 text-white'
              : 'bg-zinc-950 border-zinc-800 text-zinc-300'
          }`}
        >
          <div className="flex items-center gap-2 overflow-hidden">
            {syncNotification.type === 'success' ? (
              <CheckCircle className="w-3.5 h-3.5 text-white shrink-0" />
            ) : (
              <AlertCircle className="w-3.5 h-3.5 text-zinc-400 shrink-0" />
            )}
            <span className="font-mono text-xs truncate">{syncNotification.message}</span>
          </div>
          {onClearNotification && (
            <button
              type="button"
              onClick={onClearNotification}
              className="text-zinc-500 hover:text-white shrink-0 ml-2 p-0.5"
              title="Dismiss"
            >
              <X className="w-3.5 h-3.5" />
            </button>
          )}
        </div>
      )}

      {/* Monaco Code Editor */}
      <div className="flex-1 w-full overflow-hidden bg-black">
        <Editor
          height="100%"
          language="hcl"
          theme="vib-monochrome"
          beforeMount={handleBeforeMount}
          value={hclCode}
          onChange={(val) => {
            if (val !== undefined) {
              onChange(val)
            }
          }}
          options={{
            minimap: { enabled: false },
            fontSize: 13,
            fontFamily: "'JetBrains Mono', 'Fira Code', Consolas, monospace",
            lineNumbers: 'on',
            scrollBeyondLastLine: false,
            wordWrap: 'on',
            automaticLayout: true,
            tabSize: 2,
            renderLineHighlight: 'all',
            padding: { top: 12, bottom: 12 },
          }}
        />
      </div>
    </div>
  )
}

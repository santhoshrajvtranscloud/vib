import React, { useState } from 'react'
import {
  X,
  ShieldCheck,
  Key,
  Lock,
  Eye,
  EyeOff,
  CheckCircle2,
  RefreshCw,
  Trash2,
  Loader2,
  AlertCircle,
} from 'lucide-react'
import type { AWSIdentityInfo } from '../types'

interface AWSModalProps {
  isOpen: boolean
  onClose: () => void
  awsStatus: AWSIdentityInfo | null
  onRefreshStatus: () => Promise<void>
  onSaveCredentials: (accessKey: string, secretKey: string, sessionToken?: string) => Promise<void>
  onClearCredentials: () => Promise<void>
}

export const AWSModal: React.FC<AWSModalProps> = ({
  isOpen,
  onClose,
  awsStatus,
  onRefreshStatus,
  onSaveCredentials,
  onClearCredentials,
}) => {
  const [activeTab, setActiveTab] = useState<'status' | 'custom'>('status')
  const [accessKey, setAccessKey] = useState('')
  const [secretKey, setSecretKey] = useState('')
  const [sessionToken, setSessionToken] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [isClearing, setIsClearing] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [formSuccess, setFormSuccess] = useState<string | null>(null)

  if (!isOpen) return null

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError(null)
    setFormSuccess(null)

    if (!accessKey.trim() || !secretKey.trim()) {
      setFormError('Access Key ID and Secret Access Key are required.')
      return
    }

    try {
      setIsSubmitting(true)
      await onSaveCredentials(accessKey.trim(), secretKey.trim(), sessionToken.trim() || undefined)
      setFormSuccess('Credentials verified against AWS STS and encrypted with AES-256-GCM.')
      setAccessKey('')
      setSecretKey('')
      setSessionToken('')
      setTimeout(() => {
        setActiveTab('status')
        setFormSuccess(null)
      }, 1500)
    } catch (err: any) {
      setFormError(err.message || 'Failed to authenticate and save credentials')
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleClear = async () => {
    try {
      setIsClearing(true)
      await onClearCredentials()
      await onRefreshStatus()
    } catch (err: any) {
      setFormError(err.message || 'Failed to clear credentials')
    } finally {
      setIsClearing(false)
    }
  }

  const handleRefresh = async () => {
    setIsRefreshing(true)
    try {
      await onRefreshStatus()
    } finally {
      setIsRefreshing(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-xs">
      <div
        className="w-full max-w-lg bg-white border border-zinc-200 rounded-xl overflow-hidden flex flex-col shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="px-5 py-4 border-b border-zinc-200 flex items-center justify-between bg-zinc-50">
          <div className="flex items-center gap-2.5">
            <ShieldCheck className="w-5 h-5 text-zinc-700" />
            <div>
              <h3 className="text-sm font-semibold text-zinc-950">AWS Connection & Security</h3>
              <p className="text-xs text-zinc-500">Manage credentials and authentication</p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-md text-zinc-400 hover:text-zinc-950 hover:bg-zinc-100 transition-colors"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Tab Navigation */}
        <div className="flex border-b border-zinc-200 bg-white px-5 pt-2">
          <button
            type="button"
            onClick={() => setActiveTab('status')}
            className={`pb-2.5 px-3 text-xs font-medium border-b-2 transition-colors flex items-center gap-1.5 ${
              activeTab === 'status'
                ? 'border-zinc-950 text-zinc-950'
                : 'border-transparent text-zinc-500 hover:text-zinc-900'
            }`}
          >
            Connection Status
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('custom')}
            className={`pb-2.5 px-3 text-xs font-medium border-b-2 transition-colors flex items-center gap-1.5 ${
              activeTab === 'custom'
                ? 'border-zinc-950 text-zinc-950'
                : 'border-transparent text-zinc-500 hover:text-zinc-900'
            }`}
          >
            <Key className="w-3.5 h-3.5" />
            Custom API Keys
          </button>
        </div>

        {/* Modal Body */}
        <div className="p-5 flex-1 overflow-y-auto">
          {activeTab === 'status' && (
            <div className="space-y-4">
              {/* Connection Status Card */}
              <div className="p-4 rounded-lg border border-zinc-200 bg-zinc-50 text-zinc-800 shadow-2xs">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2.5">
                    <span className="text-sm text-zinc-950">
                      {awsStatus?.connected ? '●' : '○'}
                    </span>
                    <div>
                      <div className="text-xs font-semibold text-zinc-950">
                        {awsStatus?.connected ? 'Connected to AWS' : 'Not Connected'}
                      </div>
                      <div className="text-[11px] text-zinc-500">
                        {awsStatus?.connected
                          ? `Authenticated via ${
                              awsStatus.source === 'encrypted_store'
                                ? 'Encrypted Store (~/.vib/)'
                                : 'Local Profile / Environment (~/.aws)'
                            }`
                          : 'No active credentials discovered'}
                      </div>
                    </div>
                  </div>

                  <button
                    onClick={handleRefresh}
                    disabled={isRefreshing}
                    className="p-1.5 rounded-md text-zinc-500 hover:text-zinc-950 bg-white border border-zinc-200 hover:border-zinc-300 transition-colors shadow-2xs"
                    title="Refresh AWS STS identity"
                  >
                    <RefreshCw className={`w-3.5 h-3.5 ${isRefreshing ? 'animate-spin' : ''}`} />
                  </button>
                </div>

                {awsStatus?.connected && (
                  <div className="mt-3 pt-3 border-t border-zinc-200 space-y-1.5 text-xs font-mono">
                    {awsStatus.account_id && (
                      <div className="flex justify-between">
                        <span className="text-zinc-500 font-sans">Account ID:</span>
                        <span className="text-zinc-950 font-semibold">{awsStatus.account_id}</span>
                      </div>
                    )}
                    {awsStatus.arn && (
                      <div className="flex justify-between items-start gap-2">
                        <span className="text-zinc-500 font-sans shrink-0">Caller ARN:</span>
                        <span className="text-zinc-800 text-[11px] break-all text-right">
                          {awsStatus.arn}
                        </span>
                      </div>
                    )}
                    {awsStatus.masked_access_key && (
                      <div className="flex justify-between">
                        <span className="text-zinc-500 font-sans">Access Key:</span>
                        <span className="text-zinc-800">{awsStatus.masked_access_key}</span>
                      </div>
                    )}
                  </div>
                )}

                {awsStatus?.error && (
                  <div className="mt-3 p-2.5 rounded-md bg-white border border-zinc-300 text-zinc-800 text-[11px] shadow-2xs">
                    <div className="font-semibold text-zinc-950 mb-0.5">STS Verification Notice:</div>
                    {awsStatus.error}
                  </div>
                )}
              </div>

              {/* Encrypted Store Info */}
              {awsStatus?.source === 'encrypted_store' && (
                <div className="flex items-center justify-between p-3 rounded-lg bg-zinc-50 border border-zinc-200 text-xs shadow-2xs">
                  <div className="flex items-center gap-2">
                    <Lock className="w-4 h-4 text-zinc-500" />
                    <div>
                      <div className="font-medium text-zinc-950">Custom Encrypted Store Active</div>
                      <div className="text-[11px] text-zinc-500">Stored in ~/.vib/credentials.enc (0600)</div>
                    </div>
                  </div>
                  <button
                    onClick={handleClear}
                    disabled={isClearing}
                    className="flex items-center gap-1 text-xs border border-zinc-300 bg-white text-zinc-700 hover:text-zinc-950 hover:bg-zinc-50 px-2.5 py-1 rounded-md transition-colors shadow-2xs"
                  >
                    <Trash2 className="w-3 h-3" />
                    Revert to Default
                  </button>
                </div>
              )}

              {/* Explainer */}
              <div className="text-xs text-zinc-600 space-y-2 leading-relaxed">
                <p>
                  <strong className="text-zinc-900">Default Chain:</strong> VIB automatically respects your local{' '}
                  <code className="text-zinc-800 font-mono text-[11px] bg-zinc-100 px-1 py-0.5 rounded">~/.aws/credentials</code>, environment variables, or IAM role without requiring manual entry.
                </p>
                <p>
                  To connect without configuring CLI profiles, switch to the <strong className="text-zinc-900">Custom API Keys</strong> tab.
                </p>
              </div>
            </div>
          )}

          {activeTab === 'custom' && (
            <form onSubmit={handleSave} className="space-y-4">
              <div className="text-xs text-zinc-600 flex items-start gap-2 bg-zinc-50 p-3 rounded-lg border border-zinc-200 leading-relaxed shadow-2xs">
                <Lock className="w-4 h-4 text-zinc-500 shrink-0 mt-0.5" />
                <span>
                  Keys are validated via AWS STS and encrypted at rest with <strong className="text-zinc-950">AES-256-GCM</strong> in your local home directory (<code className="text-zinc-800 font-mono text-[11px] bg-zinc-100 px-1 py-0.5 rounded">~/.vib/</code>). Raw secrets are never saved in HCL files or returned by APIs.
                </span>
              </div>

              {formError && (
                <div className="p-3 rounded-md bg-zinc-50 border border-zinc-300 text-zinc-900 text-xs flex items-start gap-2 shadow-2xs">
                  <AlertCircle className="w-4 h-4 shrink-0 mt-0.5 text-zinc-700" />
                  <span>{formError}</span>
                </div>
              )}

              {formSuccess && (
                <div className="p-3 rounded-md bg-zinc-50 border border-zinc-950 text-zinc-950 text-xs flex items-center gap-2 shadow-2xs">
                  <CheckCircle2 className="w-4 h-4 shrink-0 text-zinc-950" />
                  <span>{formSuccess}</span>
                </div>
              )}

              <div>
                <label className="block text-xs font-medium text-zinc-700 mb-1">
                  AWS Access Key ID <span className="text-zinc-950">*</span>
                </label>
                <input
                  type="text"
                  value={accessKey}
                  onChange={(e) => setAccessKey(e.target.value)}
                  placeholder="AKIAIOSFODNN7EXAMPLE"
                  className="w-full bg-white border border-zinc-300 rounded-md px-3 py-2 text-xs text-zinc-900 placeholder:text-zinc-400 focus:border-zinc-950 focus:ring-1 focus:ring-zinc-950 outline-none font-mono shadow-2xs"
                  required
                />
              </div>

              <div>
                <label className="block text-xs font-medium text-zinc-700 mb-1">
                  AWS Secret Access Key <span className="text-zinc-950">*</span>
                </label>
                <div className="relative">
                  <input
                    type={showSecret ? 'text' : 'password'}
                    value={secretKey}
                    onChange={(e) => setSecretKey(e.target.value)}
                    placeholder="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
                    className="w-full bg-white border border-zinc-300 rounded-md px-3 py-2 pr-9 text-xs text-zinc-900 placeholder:text-zinc-400 focus:border-zinc-950 focus:ring-1 focus:ring-zinc-950 outline-none font-mono shadow-2xs"
                    required
                  />
                  <button
                    type="button"
                    onClick={() => setShowSecret(!showSecret)}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-zinc-400 hover:text-zinc-700"
                  >
                    {showSecret ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                  </button>
                </div>
              </div>

              <div>
                <label className="block text-xs font-medium text-zinc-700 mb-1">
                  Session Token (Optional, for temporary STS credentials)
                </label>
                <input
                  type="password"
                  value={sessionToken}
                  onChange={(e) => setSessionToken(e.target.value)}
                  placeholder="AQoDYXdzEJr1..."
                  className="w-full bg-white border border-zinc-300 rounded-md px-3 py-2 text-xs text-zinc-900 placeholder:text-zinc-400 focus:border-zinc-950 focus:ring-1 focus:ring-zinc-950 outline-none font-mono shadow-2xs"
                />
              </div>

              <div className="pt-2">
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="w-full py-2 px-4 rounded-md text-xs font-medium bg-zinc-950 text-white hover:bg-zinc-800 transition-colors disabled:opacity-50 flex items-center justify-center gap-2 shadow-2xs"
                >
                  {isSubmitting ? (
                    <>
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                      Verifying with STS...
                    </>
                  ) : (
                    <>
                      <ShieldCheck className="w-4 h-4" />
                      Save & Encrypt Credentials
                    </>
                  )}
                </button>
              </div>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}

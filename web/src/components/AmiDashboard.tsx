import React, { useState, useEffect, useCallback } from 'react'
import Editor from '@monaco-editor/react'
import {
  Layers,
  Search,
  RefreshCw,
  FileCode2,
  Trash2,
  X,
  Copy,
  Check,
  ArrowRight,
  AlertTriangle,
  Loader2,
  Server,
  Package,
} from 'lucide-react'
import type { BuildConfig, VIBImageItem, ImageConfigResponse } from '../types'

interface AmiDashboardProps {
  region: string
  onLoadConfigToBuilder: (config: BuildConfig, hclCode?: string) => void
}

export const AmiDashboard: React.FC<AmiDashboardProps> = ({
  region,
  onLoadConfigToBuilder,
}) => {
  const [images, setImages] = useState<VIBImageItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [copiedId, setCopiedId] = useState<string | null>(null)

  // Config Modal State
  const [selectedConfig, setSelectedConfig] = useState<ImageConfigResponse | null>(null)
  const [configModalOpen, setConfigModalOpen] = useState(false)
  const [configLoading, setConfigLoading] = useState(false)

  // Delete Confirmation State
  const [deleteTarget, setDeleteTarget] = useState<VIBImageItem | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)
  const [actionFeedback, setActionFeedback] = useState<string | null>(null)

  const fetchImages = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const res = await fetch(`/api/images?region=${region}`)
      if (!res.ok) throw new Error('Failed to load AMIs from backend')
      const data = await res.json()
      setImages(data.images || [])
    } catch (err: any) {
      setError(err.message || 'Error fetching images')
    } finally {
      setLoading(false)
    }
  }, [region])

  useEffect(() => {
    fetchImages()
  }, [fetchImages])

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text)
    setCopiedId(text)
    setTimeout(() => setCopiedId(null), 2000)
  }

  const handleOpenConfig = async (item: VIBImageItem) => {
    const idToLookup = item.build_id || item.ami_id
    try {
      setConfigLoading(true)
      setConfigModalOpen(true)
      const res = await fetch(`/api/images/${idToLookup}/config`)
      if (!res.ok) throw new Error('Could not retrieve build configuration')
      const data: ImageConfigResponse = await res.json()
      setSelectedConfig(data)
    } catch (err: any) {
      setActionFeedback(`Notice: ${err.message}`)
      setTimeout(() => setActionFeedback(null), 4000)
      setConfigModalOpen(false)
    } finally {
      setConfigLoading(false)
    }
  }

  const handleConfirmDelete = async () => {
    if (!deleteTarget) return
    const idToDelete = deleteTarget.ami_id || deleteTarget.build_id
    if (!idToDelete) return

    try {
      setIsDeleting(true)
      const res = await fetch(`/api/images/${idToDelete}?region=${deleteTarget.region || region}`, {
        method: 'DELETE',
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'Failed to delete AMI')

      setActionFeedback(data.message || 'AMI and associated snapshots deleted.')
      setTimeout(() => setActionFeedback(null), 5000)
      setDeleteTarget(null)
      await fetchImages()
    } catch (err: any) {
      setActionFeedback(`Delete Error: ${err.message}`)
      setTimeout(() => setActionFeedback(null), 5000)
    } finally {
      setIsDeleting(false)
    }
  }

  const handleLoadToBuilder = () => {
    if (!selectedConfig) return
    if (selectedConfig.config) {
      onLoadConfigToBuilder(selectedConfig.config, selectedConfig.hcl_code)
    } else {
      // Reconstitute basic config if config object missing
      const fallbackConfig: BuildConfig = {
        ami_name: selectedConfig.ami_name || 'vib-imported-app',
        region: selectedConfig.region || region,
        instance_type: 't3.micro',
        source_ami: '',
        ssh_username: 'ubuntu',
        packages: [],
      }
      onLoadConfigToBuilder(fallbackConfig, selectedConfig.hcl_code)
    }
    setConfigModalOpen(false)
  }

  const filteredImages = images.filter((img) => {
    const q = searchQuery.toLowerCase().trim()
    if (!q) return true
    return (
      (img.name && img.name.toLowerCase().includes(q)) ||
      (img.ami_id && img.ami_id.toLowerCase().includes(q)) ||
      (img.build_id && img.build_id.toLowerCase().includes(q)) ||
      (img.region && img.region.toLowerCase().includes(q))
    )
  })

  return (
    <div className="flex flex-col h-full bg-black text-white overflow-hidden">
      {/* Top Action Bar */}
      <div className="h-14 border-b border-zinc-800 bg-zinc-950 px-6 flex items-center justify-between shrink-0 select-none">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-md bg-zinc-900 border border-zinc-800 flex items-center justify-center text-zinc-300">
            <Layers className="w-4 h-4" />
          </div>
          <div>
            <h1 className="text-sm font-semibold tracking-tight text-white flex items-center gap-2">
              AMIs & Build History
              <span className="text-[11px] font-mono font-normal text-zinc-500 border border-zinc-800 bg-zinc-900 px-1.5 py-0.2 rounded">
                SQLite Persistence
              </span>
            </h1>
            <p className="text-xs text-zinc-500">
              Manage images built with VIB, inspect HCL configurations, and clean up AWS resources.
            </p>
          </div>
        </div>

        <div className="flex items-center gap-3">
          {/* Search Box */}
          <div className="relative">
            <Search className="w-3.5 h-3.5 text-zinc-500 absolute left-2.5 top-1/2 -translate-y-1/2" />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search AMI, name, build ID..."
              className="bg-zinc-900 border border-zinc-800 text-white rounded-md pl-8 pr-3 py-1.5 text-xs font-mono placeholder:text-zinc-600 focus:border-white focus:ring-0 outline-none w-64 transition-colors"
            />
          </div>

          {/* Refresh Button */}
          <button
            type="button"
            onClick={fetchImages}
            disabled={loading}
            className="flex items-center gap-1.5 border border-zinc-700 bg-zinc-900 text-zinc-300 hover:border-zinc-400 hover:text-white px-3 py-1.5 rounded-md text-xs font-medium transition-colors disabled:opacity-50"
            title="Refresh AMI and Build table"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
        </div>
      </div>

      {/* Error / Feedback Banner */}
      {error && (
        <div className="px-6 py-2.5 bg-zinc-900 border-b border-zinc-700 text-xs font-mono text-zinc-300 flex items-center justify-between animate-fadeIn">
          <span>Notice: {error}</span>
          <button
            type="button"
            onClick={() => setError(null)}
            className="text-zinc-500 hover:text-white"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {/* Action Notification Banner */}
      {actionFeedback && (
        <div className="px-6 py-2.5 bg-zinc-900 border-b border-zinc-800 text-xs font-mono text-zinc-200 flex items-center justify-between animate-fadeIn">
          <span>{actionFeedback}</span>
          <button
            type="button"
            onClick={() => setActionFeedback(null)}
            className="text-zinc-500 hover:text-white"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {/* Main Table Area */}
      <div className="flex-1 overflow-auto p-6">
        <div className="border border-zinc-800 rounded-lg overflow-hidden bg-zinc-950">
          <table className="w-full text-left border-collapse">
            <thead>
              <tr className="border-b border-zinc-800 bg-zinc-900/60 text-xs font-mono text-zinc-400">
                <th className="py-3 px-4 font-medium uppercase tracking-wider">Image / Name</th>
                <th className="py-3 px-4 font-medium uppercase tracking-wider">AMI ID</th>
                <th className="py-3 px-4 font-medium uppercase tracking-wider">Region</th>
                <th className="py-3 px-4 font-medium uppercase tracking-wider">Status</th>
                <th className="py-3 px-4 font-medium uppercase tracking-wider">Created</th>
                <th className="py-3 px-4 font-medium uppercase tracking-wider text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800/80 text-xs font-mono">
              {loading && filteredImages.length === 0 ? (
                <tr>
                  <td colSpan={6} className="py-12 text-center text-zinc-500">
                    <Loader2 className="w-5 h-5 animate-spin mx-auto mb-2 text-zinc-400" />
                    Loading AMIs from AWS EC2 and SQLite database...
                  </td>
                </tr>
              ) : filteredImages.length === 0 ? (
                <tr>
                  <td colSpan={6} className="py-12 text-center text-zinc-500">
                    <Server className="w-6 h-6 mx-auto mb-2 text-zinc-600" />
                    No VIB-created AMIs found in {region} or local database.
                    <p className="text-[11px] text-zinc-600 mt-1">
                      Build an AMI using the Builder tab to populate this dashboard.
                    </p>
                  </td>
                </tr>
              ) : (
                filteredImages.map((img) => {
                  const isDeleted = img.status === 'deleted'

                  return (
                    <tr
                      key={img.ami_id + (img.build_id || '')}
                      className="hover:bg-zinc-900/40 transition-colors"
                    >
                      {/* Name & Build ID */}
                      <td className="py-3 px-4">
                        <div className="font-sans font-medium text-white flex items-center gap-2">
                          <span className={isDeleted ? 'line-through text-zinc-500' : ''}>
                            {img.name || 'Unnamed Build'}
                          </span>
                        </div>
                        {img.build_id && (
                          <div className="text-[10px] text-zinc-500 font-mono mt-0.5">
                            Build ID: {img.build_id}
                          </div>
                        )}
                      </td>

                      {/* AMI ID */}
                      <td className="py-3 px-4">
                        {img.ami_id && img.ami_id.startsWith('ami-') ? (
                          <div className="inline-flex items-center gap-1.5 bg-zinc-900 border border-zinc-800 px-2 py-0.5 rounded text-zinc-200">
                            <span>{img.ami_id}</span>
                            <button
                              type="button"
                              onClick={() => copyToClipboard(img.ami_id)}
                              className="text-zinc-500 hover:text-white"
                              title="Copy AMI ID"
                            >
                              {copiedId === img.ami_id ? (
                                <Check className="w-3 h-3 text-white" />
                              ) : (
                                <Copy className="w-3 h-3" />
                              )}
                            </button>
                          </div>
                        ) : (
                          <span className="text-zinc-600 text-[11px]">—</span>
                        )}
                      </td>

                      {/* Region */}
                      <td className="py-3 px-4 text-zinc-300">
                        {img.region || region}
                      </td>

                      {/* Status */}
                      <td className="py-3 px-4">
                        {img.live_in_aws ? (
                          <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-medium bg-zinc-900 border border-zinc-700 text-white">
                            <span className="w-1.5 h-1.5 rounded-full bg-white"></span>
                            Live in AWS
                          </span>
                        ) : img.status === 'success' ? (
                          <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-medium bg-zinc-900 border border-zinc-800 text-zinc-300">
                            Built
                          </span>
                        ) : img.status === 'deleted' ? (
                          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-medium bg-zinc-950 border border-zinc-800 text-zinc-500 line-through">
                            Deleted
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-medium bg-zinc-950 border border-zinc-800 text-zinc-400 capitalize">
                            {img.status}
                          </span>
                        )}
                      </td>

                      {/* Created At */}
                      <td className="py-3 px-4 text-zinc-400 text-[11px]">
                        {img.creation_date
                          ? new Date(img.creation_date).toLocaleDateString([], {
                              year: 'numeric',
                              month: 'short',
                              day: 'numeric',
                              hour: '2-digit',
                              minute: '2-digit',
                            })
                          : '—'}
                      </td>

                      {/* Actions */}
                      <td className="py-3 px-4 text-right">
                        <div className="inline-flex items-center gap-2">
                          <button
                            type="button"
                            onClick={() => handleOpenConfig(img)}
                            className="border border-zinc-800 bg-zinc-900 hover:border-zinc-600 text-zinc-300 hover:text-white px-2.5 py-1 rounded-md text-xs font-sans transition-colors flex items-center gap-1"
                            title="Inspect generated HCL and input configuration"
                          >
                            <FileCode2 className="w-3.5 h-3.5" />
                            View Config
                          </button>

                          {!isDeleted && (
                            <button
                              type="button"
                              onClick={() => setDeleteTarget(img)}
                              className="border border-zinc-800 bg-zinc-950 hover:border-zinc-600 text-zinc-500 hover:text-white px-2 py-1 rounded-md text-xs font-sans transition-colors flex items-center gap-1"
                              title="Deregister AMI and delete EBS snapshots"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* View Config Modal */}
      {configModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
          <div
            className="w-full max-w-4xl h-[650px] bg-zinc-950 border border-zinc-800 rounded-xl overflow-hidden flex flex-col shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Modal Header */}
            <div className="px-5 py-3.5 border-b border-zinc-800 flex items-center justify-between bg-zinc-900/60">
              <div className="flex items-center gap-2.5">
                <FileCode2 className="w-4 h-4 text-zinc-300" />
                <div>
                  <h3 className="text-sm font-semibold text-white">
                    {selectedConfig?.ami_name || 'Build Configuration & HCL'}
                  </h3>
                  <p className="text-xs text-zinc-500 font-mono">
                    {selectedConfig?.ami_id || selectedConfig?.build_id}
                  </p>
                </div>
              </div>

              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={handleLoadToBuilder}
                  className="flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-white text-black hover:bg-zinc-200 transition-colors"
                >
                  <ArrowRight className="w-3.5 h-3.5" />
                  Load into Builder
                </button>
                <button
                  onClick={() => setConfigModalOpen(false)}
                  className="p-1 rounded-md text-zinc-400 hover:text-white hover:bg-zinc-900 transition-colors"
                >
                  <X className="w-4 h-4" />
                </button>
              </div>
            </div>

            {/* Modal Body */}
            {configLoading ? (
              <div className="flex-1 flex flex-col items-center justify-center text-zinc-400 gap-2">
                <Loader2 className="w-6 h-6 animate-spin text-white" />
                <span className="text-xs font-mono">Loading configuration from SQLite...</span>
              </div>
            ) : (
              <div className="flex-1 flex overflow-hidden">
                {/* Left Column: Parsed Specs */}
                <div className="w-72 border-r border-zinc-800 p-4 overflow-y-auto space-y-4 bg-zinc-950 text-xs">
                <div>
                  <h4 className="text-[11px] font-semibold uppercase font-mono text-zinc-400 tracking-wider mb-2">
                    Configuration Parameters
                  </h4>
                  <div className="space-y-2 font-mono text-zinc-300">
                    <div>
                      <span className="text-zinc-500 block text-[10px]">REGION</span>
                      <span>{selectedConfig?.region}</span>
                    </div>
                    {selectedConfig?.config?.instance_type && (
                      <div>
                        <span className="text-zinc-500 block text-[10px]">INSTANCE TYPE</span>
                        <span>{selectedConfig.config.instance_type}</span>
                      </div>
                    )}
                    {selectedConfig?.config?.source_ami && (
                      <div>
                        <span className="text-zinc-500 block text-[10px]">SOURCE AMI</span>
                        <span className="text-[11px] break-all">{selectedConfig.config.source_ami}</span>
                      </div>
                    )}
                    {selectedConfig?.config?.ssh_username && (
                      <div>
                        <span className="text-zinc-500 block text-[10px]">SSH USERNAME</span>
                        <span>{selectedConfig.config.ssh_username}</span>
                      </div>
                    )}
                    {selectedConfig?.duration_seconds ? (
                      <div>
                        <span className="text-zinc-500 block text-[10px]">BUILD TIME</span>
                        <span>{selectedConfig.duration_seconds}s</span>
                      </div>
                    ) : null}
                  </div>
                </div>

                {selectedConfig?.config?.packages && selectedConfig.config.packages.length > 0 && (
                  <div>
                    <h4 className="text-[11px] font-semibold uppercase font-mono text-zinc-400 tracking-wider mb-1.5 flex items-center gap-1">
                      <Package className="w-3 h-3 text-zinc-400" />
                      Packages Installed ({selectedConfig.config.packages.length})
                    </h4>
                    <div className="flex flex-wrap gap-1">
                      {selectedConfig.config.packages.map((pkg) => (
                        <span
                          key={pkg}
                          className="bg-zinc-900 border border-zinc-800 text-zinc-200 px-2 py-0.5 rounded text-[11px] font-mono"
                        >
                          {pkg}
                        </span>
                      ))}
                    </div>
                  </div>
                )}
              </div>

              {/* Right Column: Read-Only Monaco Editor */}
              <div className="flex-1 bg-black overflow-hidden flex flex-col">
                <div className="h-8 border-b border-zinc-800 bg-zinc-900/50 px-3 flex items-center text-[11px] font-mono text-zinc-400 select-none">
                  aws-build.pkr.hcl (read-only)
                </div>
                <div className="flex-1 w-full overflow-hidden">
                  <Editor
                    height="100%"
                    language="hcl"
                    theme="vs-dark"
                    value={selectedConfig?.hcl_code || '// No HCL template available'}
                    options={{
                      readOnly: true,
                      minimap: { enabled: false },
                      fontSize: 13,
                      fontFamily: "'JetBrains Mono', Consolas, monospace",
                      lineNumbers: 'on',
                      scrollBeyondLastLine: false,
                      automaticLayout: true,
                    }}
                  />
                </div>
              </div>
            </div>
            )}
          </div>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      {deleteTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
          <div
            className="w-full max-w-md bg-zinc-950 border border-zinc-800 rounded-xl p-5 shadow-2xl flex flex-col space-y-4"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-start gap-3">
              <div className="w-8 h-8 rounded-md bg-zinc-900 border border-zinc-700 flex items-center justify-center shrink-0 text-white">
                <AlertTriangle className="w-4 h-4" />
              </div>
              <div>
                <h3 className="text-sm font-semibold text-white">
                  Delete VIB Machine Image?
                </h3>
                <p className="text-xs text-zinc-400 mt-1 leading-relaxed">
                  This action will call AWS EC2 <code className="text-white font-mono">DeregisterImage</code> and permanently delete all associated EBS snapshots to stop storage costs.
                </p>
                <div className="mt-2.5 p-2 rounded bg-zinc-900 border border-zinc-800 font-mono text-xs text-zinc-300">
                  <div>Name: <span className="text-white">{deleteTarget.name}</span></div>
                  {deleteTarget.ami_id && <div>AMI: <span className="text-white">{deleteTarget.ami_id}</span></div>}
                  <div>Region: <span className="text-white">{deleteTarget.region || region}</span></div>
                </div>
              </div>
            </div>

            <div className="flex items-center justify-end gap-2 pt-2 border-t border-zinc-800">
              <button
                type="button"
                onClick={() => setDeleteTarget(null)}
                disabled={isDeleting}
                className="px-3 py-1.5 rounded-md border border-zinc-700 bg-transparent text-zinc-300 hover:border-zinc-400 hover:text-white text-xs font-medium transition-colors"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={handleConfirmDelete}
                disabled={isDeleting}
                className="px-3.5 py-1.5 rounded-md bg-white text-black hover:bg-zinc-200 text-xs font-medium transition-colors flex items-center gap-1.5 disabled:opacity-50"
              >
                {isDeleting ? (
                  <>
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    Deleting from AWS...
                  </>
                ) : (
                  'Deregister & Delete'
                )}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

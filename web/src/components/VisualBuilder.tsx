import React, { useState, useEffect } from 'react'
import {
  Copy,
  Check,
  ChevronDown,
  ChevronUp,
  Info,
  Layers,
  Settings,
  ShieldCheck,
  Plus,
  X,
  Code2,
  Network,
  RefreshCw,
  Search,
  ArrowRight,
  ArrowLeft,
  Play,
  CheckCircle,
} from 'lucide-react'
import type { BuildConfig, CataloguePackage, BaseAMI, NetworksResponse, BuildStatusSummary } from '../types'
import { apiFetch } from '../api'

interface VisualBuilderProps {
  config: BuildConfig
  onChange: (updated: BuildConfig) => void
  catalogue: CataloguePackage[]
  catalogueLoading: boolean
  isManualMode: boolean
  onResetToForm: () => void
  baseAMIs: BaseAMI[]
  baseAMIsLoading: boolean
  baseAMISource: string
  architecture: string
  onArchChange: (arch: string) => void
  onBuild?: () => void
  onValidate?: () => void
  onPreflight?: () => void
  currentBuild?: BuildStatusSummary | null
}

const INSTANCE_OPTIONS = [
  { value: 't3.micro', label: 't3.micro (2 vCPU, 1 GiB RAM) — Free Tier' },
  { value: 't3.small', label: 't3.small (2 vCPU, 2 GiB RAM)' },
  { value: 't3.medium', label: 't3.medium (2 vCPU, 4 GiB RAM)' },
  { value: 't3.large', label: 't3.large (2 vCPU, 8 GiB RAM)' },
  { value: 'c5.large', label: 'c5.large (2 vCPU, 4 GiB RAM) — Compute Optimized' },
  { value: 'm5.large', label: 'm5.large (2 vCPU, 8 GiB RAM) — General Purpose' },
  { value: 't4g.micro', label: 't4g.micro (2 vCPU, 1 GiB RAM) — AWS Graviton (arm64)' },
  { value: 't4g.small', label: 't4g.small (2 vCPU, 2 GiB RAM) — AWS Graviton (arm64)' },
]

interface OSQuickOption {
  key: string
  title: string
  family: string
  description: string
  defaultUser: string
  ownerName: string
  expectedOwner: string
}

const OS_OPTIONS: OSQuickOption[] = [
  {
    key: 'ubuntu-24.04',
    title: 'Ubuntu 24.04 LTS',
    family: 'Canonical Ubuntu',
    description: 'Noble Numbat · Linux 6.8 · systemd',
    defaultUser: 'ubuntu',
    ownerName: 'Canonical',
    expectedOwner: '099720109477',
  },
  {
    key: 'ubuntu-22.04',
    title: 'Ubuntu 22.04 LTS',
    family: 'Canonical Ubuntu',
    description: 'Jammy Jellyfish · Linux 5.15 · systemd',
    defaultUser: 'ubuntu',
    ownerName: 'Canonical',
    expectedOwner: '099720109477',
  },
  {
    key: 'amazon-linux-2023',
    title: 'Amazon Linux 2023',
    family: 'Amazon Web Services',
    description: 'AL2023 · AWS EC2 optimized kernel',
    defaultUser: 'ec2-user',
    ownerName: 'Amazon',
    expectedOwner: '137112412989',
  },
  {
    key: 'debian-12',
    title: 'Debian 12',
    family: 'Debian Cloud Team',
    description: 'Bookworm · Official minimal cloud image',
    defaultUser: 'admin',
    ownerName: 'Debian',
    expectedOwner: '136693071363',
  },
  {
    key: 'custom',
    title: 'Custom AMI',
    family: 'User Defined',
    description: 'Specify an arbitrary or private AMI ID',
    defaultUser: 'ubuntu',
    ownerName: 'Private / Custom',
    expectedOwner: 'Self / Organization',
  },
]

export const VisualBuilder: React.FC<VisualBuilderProps> = ({
  config,
  onChange,
  catalogue,
  catalogueLoading,
  isManualMode,
  onResetToForm,
  baseAMIs,
  baseAMIsLoading,
  baseAMISource,
  architecture,
  onArchChange,
  onBuild,
  onValidate,
  onPreflight,
  currentBuild,
}) => {
  const [currentStep, setCurrentStep] = useState<1 | 2 | 3>(1)
  const [selectedOSKey, setSelectedOSKey] = useState<string>('ubuntu-24.04')
  const [packageSearch, setPackageSearch] = useState('')
  const [newCustomPkg, setNewCustomPkg] = useState('')
  const [expandedPkg, setExpandedPkg] = useState<string | null>(null)
  const [customScriptText, setCustomScriptText] = useState((config.custom_script || []).join('\n'))
  const [showNetworkSection, setShowNetworkSection] = useState(Boolean(config.vpc_id || config.subnet_id))
  const [networks, setNetworks] = useState<NetworksResponse | null>(null)
  const [loadingNetworks, setLoadingNetworks] = useState(false)
  const [networkError, setNetworkError] = useState<string | null>(null)
  const [copiedField, setCopiedField] = useState<string | null>(null)

  // Identify selected OS option from config.source_ami
  useEffect(() => {
    const matched = baseAMIs.find((a) => a.id === config.source_ami)
    if (matched) {
      setSelectedOSKey(matched.os)
    } else if (config.source_ami && !baseAMIsLoading) {
      setSelectedOSKey('custom')
    }
  }, [config.source_ami, baseAMIs, baseAMIsLoading])

  const copyToClipboard = (text: string, fieldName: string) => {
    navigator.clipboard.writeText(text)
    setCopiedField(fieldName)
    setTimeout(() => setCopiedField(null), 2000)
  }

  const fetchNetworks = async (region: string) => {
    try {
      setLoadingNetworks(true)
      setNetworkError(null)
      const res = await apiFetch(`/api/aws/networks?region=${region}`)
      if (!res.ok) throw new Error(`Failed to load networks (${res.status})`)
      const data: NetworksResponse = await res.json()
      setNetworks(data)
    } catch (err: any) {
      setNetworkError(err.message || 'Failed to discover AWS VPCs')
    } finally {
      setLoadingNetworks(false)
    }
  }

  const toggleNetworkSection = () => {
    const next = !showNetworkSection
    setShowNetworkSection(next)
    if (next && !networks) {
      fetchNetworks(config.region)
    }
  }

  useEffect(() => {
    if (showNetworkSection) {
      fetchNetworks(config.region)
    }
  }, [config.region])

  const updateField = <K extends keyof BuildConfig>(key: K, value: BuildConfig[K]) => {
    onChange({
      ...config,
      [key]: value,
    })
  }

  const handleSelectOS = (opt: OSQuickOption) => {
    setSelectedOSKey(opt.key)
    if (opt.key === 'custom') {
      updateField('ssh_username', opt.defaultUser)
      return
    }

    const matched = baseAMIs.find((a) => a.os === opt.key)
    if (matched) {
      onChange({
        ...config,
        source_ami: matched.id,
        ssh_username: matched.default_user,
      })
    }
  }

  const togglePackage = (pkgId: string) => {
    const current = config.packages || []
    if (current.includes(pkgId)) {
      updateField(
        'packages',
        current.filter((p) => p !== pkgId)
      )
    } else {
      updateField('packages', [...current, pkgId])
    }
  }

  const addCustomPackage = () => {
    const trimmed = newCustomPkg.trim().toLowerCase()
    if (!trimmed) return
    const current = config.packages || []
    if (!current.includes(trimmed)) {
      updateField('packages', [...current, trimmed])
    }
    setNewCustomPkg('')
  }

  const handleCustomScriptChange = (text: string) => {
    setCustomScriptText(text)
    const lines = text
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l.length > 0)
    updateField('custom_script', lines)
  }

  const activeMatchedAMI = baseAMIs.find((a) => a.os === selectedOSKey)
  const currentOwnerID = activeMatchedAMI?.owner_id || OS_OPTIONS.find((o) => o.key === selectedOSKey)?.expectedOwner || ''

  const isBuilding =
    currentBuild?.status === 'running' ||
    currentBuild?.status === 'pending' ||
    currentBuild?.status === 'cancelling'

  const filteredCatalogue = catalogue.filter((pkg) => {
    if (!packageSearch.trim()) return true
    const q = packageSearch.toLowerCase()
    return pkg.name.toLowerCase().includes(q) || pkg.id.toLowerCase().includes(q) || pkg.description.toLowerCase().includes(q)
  })

  return (
    <div className="flex flex-col h-full bg-zinc-50 overflow-y-auto select-none">
      {/* 3-Stage Guided Stepper Header */}
      <div className="p-4 border-b border-zinc-200 bg-white sticky top-0 z-10 shadow-2xs">
        <div className="flex items-center justify-between mb-3">
          <div className="flex items-center gap-2">
            <span className="text-xs font-bold uppercase tracking-wider text-zinc-950">
              Build Workflow
            </span>
            <span className="text-[11px] font-mono text-zinc-500">
              Step {currentStep} of 3
            </span>
          </div>

          {isManualMode ? (
            <button
              onClick={onResetToForm}
              className="text-[11px] font-medium border border-zinc-300 bg-white text-zinc-800 hover:bg-zinc-50 hover:border-zinc-400 px-2 py-0.5 rounded-md transition-colors shadow-2xs"
            >
              Reset to Form
            </button>
          ) : (
            <span className="text-[10px] font-mono text-zinc-600 border border-zinc-200 bg-zinc-50 px-2 py-0.5 rounded shadow-2xs">
              Live Sync
            </span>
          )}
        </div>

        {/* Horizontal Stepper Bar */}
        <div className="grid grid-cols-3 gap-2">
          {[
            { step: 1, label: '1. Base image' },
            { step: 2, label: '2. Config & software' },
            { step: 3, label: '3. Review & build' },
          ].map((s) => {
            const isActive = currentStep === s.step
            const isCompleted = currentStep > s.step

            return (
              <button
                key={s.step}
                type="button"
                onClick={() => setCurrentStep(s.step as 1 | 2 | 3)}
                className={`py-1.5 px-2 text-center rounded text-xs font-mono transition-all border ${
                  isActive
                    ? 'border-zinc-950 bg-zinc-950 text-white font-semibold shadow-2xs'
                    : isCompleted
                    ? 'border-zinc-300 bg-white text-zinc-900 hover:border-zinc-400'
                    : 'border-zinc-200 bg-zinc-50 text-zinc-500 hover:text-zinc-900'
                }`}
              >
                {s.label}
              </button>
            )
          })}
        </div>
      </div>

      {/* Manual HCL Mode Alert Banner */}
      {isManualMode && (
        <div className="m-4 mb-0 text-xs text-zinc-700 bg-white border border-zinc-200 rounded-md p-2.5 flex items-start gap-2 shadow-2xs">
          <Info className="w-3.5 h-3.5 shrink-0 text-zinc-500 mt-0.5" />
          <span>
            Manual HCL editing is active. Form modifications won&apos;t overwrite your editor code until you click &quot;Reset to Form&quot;.
          </span>
        </div>
      )}

      {/* Main Step Content */}
      <div className="p-4 flex-1">
        {/* ========================================================================= */}
        {/* STAGE 1: Base image                                                       */}
        {/* ========================================================================= */}
        {currentStep === 1 && (
          <div className="space-y-5">
            <div>
              <h3 className="text-xs font-bold uppercase tracking-wider text-zinc-950 flex items-center gap-1.5 mb-1">
                <Layers className="w-3.5 h-3.5 text-zinc-600" />
                Select Operating System
              </h3>
              <p className="text-[11px] text-zinc-500">
                Official cloud base images verified directly from vendor AWS accounts via EC2 DescribeImages.
              </p>
            </div>

            {/* OS Quick Start Grid (ZERO LOGOS - PURE TYPOGRAPHY & TEXT CARDS) */}
            <div className="grid grid-cols-1 gap-2">
              {OS_OPTIONS.map((opt) => {
                const isSelected = selectedOSKey === opt.key

                return (
                  <div
                    key={opt.key}
                    onClick={() => handleSelectOS(opt)}
                    className={`p-3 rounded-md border cursor-pointer transition-all ${
                      isSelected
                        ? 'border-zinc-950 bg-white ring-1 ring-zinc-950 shadow-2xs'
                        : 'border-zinc-200 bg-white text-zinc-700 hover:border-zinc-300 hover:bg-zinc-50/50'
                    }`}
                  >
                    <div className="flex items-start justify-between">
                      <div className="flex items-start gap-3">
                        <div
                          className={`mt-0.5 w-4 h-4 rounded-full border flex items-center justify-center shrink-0 transition-colors ${
                            isSelected ? 'border-zinc-950 bg-zinc-950' : 'border-zinc-300 bg-white'
                          }`}
                        >
                          {isSelected && <div className="w-1.5 h-1.5 rounded-full bg-white"></div>}
                        </div>
                        <div>
                          <div className="text-xs font-semibold text-zinc-950 flex items-center gap-2">
                            <span>{opt.title}</span>
                            <span className="text-[10px] font-mono text-zinc-500 bg-zinc-50 border border-zinc-200 px-1.5 py-0.2 rounded">
                              {opt.family}
                            </span>
                          </div>
                          <p className="text-[11px] text-zinc-500 mt-0.5 leading-relaxed">
                            {opt.description}
                          </p>
                        </div>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>

            {/* AWS-Style AMI Metadata Strip */}
            <div className="p-3.5 bg-white border border-zinc-200 rounded-lg space-y-3 shadow-2xs">
              <div className="flex items-center justify-between border-b border-zinc-100 pb-2">
                <span className="text-xs font-semibold text-zinc-950">AMI Metadata & Details</span>
                {baseAMIsLoading ? (
                  <span className="text-[10px] font-mono text-zinc-500">Querying live EC2...</span>
                ) : (
                  <span className="text-[10px] font-mono text-zinc-700 bg-zinc-100 border border-zinc-200 px-1.5 py-0.5 rounded">
                    {baseAMISource === 'aws_live' ? '● Verified Provider' : '○ Standalone'}
                  </span>
                )}
              </div>

              {/* Architecture Selector */}
              <div>
                <label className="block text-[11px] font-medium text-zinc-700 mb-1">
                  Architecture
                </label>
                <div className="flex items-center bg-zinc-100 rounded-md p-0.5 border border-zinc-200 text-xs">
                  <button
                    type="button"
                    onClick={() => onArchChange('x86_64')}
                    className={`flex-1 py-1 rounded font-mono transition-colors ${
                      architecture === 'x86_64'
                        ? 'bg-white text-zinc-950 font-semibold shadow-2xs'
                        : 'text-zinc-600 hover:text-zinc-950'
                    }`}
                  >
                    x86_64 (64-bit AMD/Intel)
                  </button>
                  <button
                    type="button"
                    onClick={() => onArchChange('arm64')}
                    className={`flex-1 py-1 rounded font-mono transition-colors ${
                      architecture === 'arm64'
                        ? 'bg-white text-zinc-950 font-semibold shadow-2xs'
                        : 'text-zinc-600 hover:text-zinc-950'
                    }`}
                  >
                    arm64 (AWS Graviton)
                  </button>
                </div>
              </div>

              {/* Resolved Source AMI ID */}
              <div>
                <label className="block text-[11px] font-medium text-zinc-700 mb-1">
                  Source AMI ID
                </label>
                {selectedOSKey === 'custom' ? (
                  <input
                    type="text"
                    value={config.source_ami}
                    onChange={(e) => updateField('source_ami', e.target.value)}
                    placeholder="ami-0123456789abcdef0"
                    className="w-full bg-white border border-zinc-300 rounded-md px-2.5 py-1.5 text-xs text-zinc-900 font-mono outline-none focus:border-zinc-950 focus:ring-1 focus:ring-zinc-950"
                  />
                ) : (
                  <div className="flex items-center justify-between bg-zinc-50 border border-zinc-200 rounded-md px-2.5 py-1.5 text-xs font-mono">
                    <span className="text-zinc-950 font-medium">
                      {config.source_ami || 'Querying EC2...'}
                    </span>
                    {config.source_ami && (
                      <button
                        type="button"
                        onClick={() => copyToClipboard(config.source_ami, 'ami_id')}
                        className="text-zinc-400 hover:text-zinc-950 ml-2"
                        title="Copy AMI ID"
                      >
                        {copiedField === 'ami_id' ? (
                          <Check className="w-3.5 h-3.5 text-zinc-950" />
                        ) : (
                          <Copy className="w-3.5 h-3.5" />
                        )}
                      </button>
                    )}
                  </div>
                )}
              </div>

              {/* Owner ID Field */}
              <div>
                <label className="block text-[11px] font-medium text-zinc-700 mb-1">
                  Publisher / Owner ID
                </label>
                <div className="flex items-center justify-between bg-zinc-50 border border-zinc-200 rounded-md px-2.5 py-1.5 text-xs font-mono">
                  <span className="text-zinc-800">
                    {currentOwnerID || 'Official AWS Account'}
                  </span>
                  {currentOwnerID && (
                    <button
                      type="button"
                      onClick={() => copyToClipboard(currentOwnerID, 'owner_id')}
                      className="text-zinc-400 hover:text-zinc-950 ml-2"
                      title="Copy Owner ID"
                    >
                      {copiedField === 'owner_id' ? (
                        <Check className="w-3.5 h-3.5 text-zinc-950" />
                      ) : (
                        <Copy className="w-3.5 h-3.5" />
                      )}
                    </button>
                  )}
                </div>
              </div>

              {/* SSH Username */}
              <div>
                <label className="block text-[11px] font-medium text-zinc-700 mb-1">
                  Default SSH User
                </label>
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={config.ssh_username}
                    onChange={(e) => updateField('ssh_username', e.target.value)}
                    className="flex-1 bg-white border border-zinc-300 rounded-md px-2.5 py-1.5 text-xs font-mono text-zinc-900 outline-none focus:border-zinc-950"
                  />
                  {(['ubuntu', 'ec2-user', 'admin'] as const).map((user) => (
                    <button
                      key={user}
                      type="button"
                      onClick={() => updateField('ssh_username', user)}
                      className={`text-[11px] font-mono px-2 py-1 rounded border transition-colors ${
                        config.ssh_username === user
                          ? 'border-zinc-950 bg-zinc-950 text-white font-medium'
                          : 'border-zinc-300 bg-white text-zinc-700 hover:bg-zinc-50'
                      }`}
                    >
                      {user}
                    </button>
                  ))}
                </div>
              </div>
            </div>

            {/* Forward Button to Step 2 */}
            <div className="pt-2">
              <button
                type="button"
                onClick={() => setCurrentStep(2)}
                className="w-full py-2.5 px-4 rounded-md text-xs font-medium bg-zinc-950 text-white hover:bg-zinc-800 transition-colors flex items-center justify-center gap-2 shadow-2xs"
              >
                <span>Next: Config & Software</span>
                <ArrowRight className="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        )}

        {/* ========================================================================= */}
        {/* STAGE 2: Config & software                                                */}
        {/* ========================================================================= */}
        {currentStep === 2 && (
          <div className="space-y-5">
            {/* Target AMI Name */}
            <div>
              <label className="block text-xs font-bold uppercase tracking-wider text-zinc-950 mb-1">
                Target AMI Name <span className="text-zinc-950">*</span>
              </label>
              <input
                type="text"
                value={config.ami_name}
                onChange={(e) => updateField('ami_name', e.target.value)}
                placeholder="e.g. vib-ubuntu-nginx-app"
                className="w-full bg-white border border-zinc-300 text-zinc-900 focus:border-zinc-950 focus:ring-1 focus:ring-zinc-950 outline-none rounded-md px-3 py-2 text-xs font-mono placeholder:text-zinc-400 transition-colors shadow-2xs"
              />
              <p className="mt-1 text-[11px] text-zinc-500">
                VIB appends <code className="text-zinc-700 font-mono">-&#123;&#123;timestamp&#125;&#125;</code> automatically to ensure uniqueness in AWS.
              </p>
            </div>

            {/* Build Instance Type */}
            <div>
              <label className="block text-xs font-bold uppercase tracking-wider text-zinc-950 mb-1">
                Build Instance Type <span className="text-zinc-950">*</span>
              </label>
              <select
                value={config.instance_type}
                onChange={(e) => updateField('instance_type', e.target.value)}
                className="w-full bg-white border border-zinc-300 text-zinc-900 focus:border-zinc-950 focus:ring-1 focus:ring-zinc-950 outline-none rounded-md px-3 py-2 text-xs font-mono transition-colors shadow-2xs"
              >
                {INSTANCE_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </div>

            {/* Software Packages Grid (NO BRAND LOGOS - TEXT & CHECKBOXES ONLY) */}
            <div className="space-y-3 pt-2 border-t border-zinc-200">
              <div className="flex items-center justify-between">
                <div>
                  <h4 className="text-xs font-bold uppercase tracking-wider text-zinc-950">
                    Software Packages
                  </h4>
                  <p className="text-[11px] text-zinc-500">
                    Select packages to install during AMI creation.
                  </p>
                </div>
                <span className="text-[11px] font-mono text-zinc-600 bg-white border border-zinc-200 px-2 py-0.5 rounded shadow-2xs">
                  {config.packages.length} selected
                </span>
              </div>

              {/* Search Packages Input */}
              <div className="relative">
                <Search className="w-3.5 h-3.5 text-zinc-400 absolute left-2.5 top-1/2 -translate-y-1/2" />
                <input
                  type="text"
                  value={packageSearch}
                  onChange={(e) => setPackageSearch(e.target.value)}
                  placeholder="Filter packages..."
                  className="w-full bg-white border border-zinc-300 rounded-md pl-8 pr-3 py-1.5 text-xs text-zinc-900 placeholder:text-zinc-400 outline-none focus:border-zinc-950 shadow-2xs"
                />
              </div>

              {/* Package Cards List */}
              {catalogueLoading ? (
                <div className="text-xs text-zinc-500 py-3 text-center">Loading catalogue...</div>
              ) : (
                <div className="space-y-2">
                  {filteredCatalogue.map((pkg) => {
                    const isSelected = config.packages.includes(pkg.id)
                    const isExpanded = expandedPkg === pkg.id

                    return (
                      <div
                        key={pkg.id}
                        className={`rounded-md border transition-all ${
                          isSelected
                            ? 'border-zinc-950 bg-white ring-1 ring-zinc-950 shadow-2xs'
                            : 'border-zinc-200 bg-white text-zinc-700 hover:border-zinc-300 hover:bg-zinc-50/50'
                        }`}
                      >
                        <div
                          className="p-3 flex items-start justify-between cursor-pointer select-none"
                          onClick={() => togglePackage(pkg.id)}
                        >
                          <div className="flex items-start gap-2.5">
                            <input
                              type="checkbox"
                              checked={isSelected}
                              onChange={() => {}} // handled by parent div
                              className="mt-0.5 w-4 h-4 rounded border-zinc-300 text-zinc-950 accent-zinc-950 focus:ring-0 cursor-pointer"
                            />
                            <div>
                              <div className="text-xs font-semibold text-zinc-950 flex items-center gap-2">
                                <span>{pkg.name}</span>
                                <span className="text-[10px] font-mono text-zinc-500 bg-zinc-50 border border-zinc-200 px-1.5 py-0.2 rounded">
                                  {pkg.id}
                                </span>
                              </div>
                              <p className="text-[11px] text-zinc-500 mt-0.5 leading-relaxed">
                                {pkg.description}
                              </p>
                            </div>
                          </div>

                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation()
                              setExpandedPkg(isExpanded ? null : pkg.id)
                            }}
                            className="text-zinc-400 hover:text-zinc-900 p-0.5 rounded"
                            title="Inspect commands"
                          >
                            {isExpanded ? (
                              <ChevronUp className="w-3.5 h-3.5" />
                            ) : (
                              <ChevronDown className="w-3.5 h-3.5" />
                            )}
                          </button>
                        </div>

                        {/* Collapsible Command Details */}
                        {isExpanded && (
                          <div className="px-3 pb-3 pt-2 border-t border-zinc-100 bg-zinc-50/80 rounded-b-md">
                            <div className="text-[10px] uppercase font-mono text-zinc-500 mb-1 flex items-center gap-1">
                              <Code2 className="w-3 h-3 text-zinc-500" />
                              Commands Executed:
                            </div>
                            <pre className="text-[11px] font-mono text-zinc-800 bg-white p-2 rounded border border-zinc-200 overflow-x-auto shadow-2xs">
                              {pkg.commands.join('\n')}
                            </pre>
                          </div>
                        )}
                      </div>
                    )
                  })}
                </div>
              )}

              {/* Add Custom Apt Package */}
              <div className="pt-1">
                <label className="block text-xs font-medium text-zinc-700 mb-1">
                  Add Custom Apt Package
                </label>
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={newCustomPkg}
                    onChange={(e) => setNewCustomPkg(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        addCustomPackage()
                      }
                    }}
                    placeholder="e.g. htop, curl, jq, ripgrep"
                    className="flex-1 bg-white border border-zinc-300 rounded-md px-3 py-1.5 text-xs text-zinc-900 placeholder:text-zinc-400 focus:border-zinc-950 focus:ring-1 focus:ring-zinc-950 outline-none font-mono shadow-2xs"
                  />
                  <button
                    type="button"
                    onClick={addCustomPackage}
                    className="border border-zinc-300 text-zinc-800 hover:border-zinc-400 hover:bg-zinc-50 bg-white px-3 py-1.5 rounded-md text-xs font-medium flex items-center gap-1 transition-colors shadow-2xs"
                  >
                    <Plus className="w-3.5 h-3.5" />
                    Add
                  </button>
                </div>

                {/* Custom packages list pills */}
                {config.packages.filter((p) => !catalogue.some((cat) => cat.id === p)).length > 0 && (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {config.packages
                      .filter((p) => !catalogue.some((cat) => cat.id === p))
                      .map((customPkg) => (
                        <span
                          key={customPkg}
                          className="inline-flex items-center gap-1 text-[11px] font-mono bg-white text-zinc-800 border border-zinc-200 px-2 py-0.5 rounded-md shadow-2xs"
                        >
                          {customPkg}
                          <button
                            type="button"
                            onClick={() => togglePackage(customPkg)}
                            className="text-zinc-400 hover:text-zinc-900 ml-0.5"
                          >
                            <X className="w-3 h-3" />
                          </button>
                        </span>
                      ))}
                  </div>
                )}
              </div>

              {/* Custom Shell Script Textarea */}
              <div className="pt-2">
                <label className="block text-xs font-medium text-zinc-700 mb-1">
                  Custom Shell Provisioner Script (one command per line)
                </label>
                <textarea
                  rows={3}
                  value={customScriptText}
                  onChange={(e) => handleCustomScriptChange(e.target.value)}
                  placeholder={`echo "Deploying custom application..."\nsudo mkdir -p /var/www/app`}
                  className="w-full bg-white border border-zinc-300 text-zinc-900 focus:border-zinc-950 focus:ring-1 focus:ring-zinc-950 outline-none rounded-md p-2.5 text-xs font-mono placeholder:text-zinc-400 transition-colors resize-y shadow-2xs"
                />
              </div>
            </div>

            {/* Collapsible Advanced Network (VPC / Subnet) */}
            <div className="pt-2 border-t border-zinc-200">
              <button
                type="button"
                onClick={toggleNetworkSection}
                className="w-full flex items-center justify-between text-xs text-zinc-700 hover:text-zinc-950 p-2.5 rounded-md bg-white border border-zinc-200 hover:border-zinc-300 transition-colors shadow-2xs"
              >
                <div className="flex items-center gap-2">
                  <Network className="w-3.5 h-3.5 text-zinc-500" />
                  <span className="font-semibold text-zinc-800">Advanced Network (VPC / Subnet)</span>
                  {config.vpc_id && (
                    <span className="text-[10px] font-mono text-zinc-900 bg-zinc-100 border border-zinc-300 px-1.5 py-0.5 rounded">
                      Custom VPC
                    </span>
                  )}
                </div>
                <div className="flex items-center gap-1 text-[11px] text-zinc-500">
                  <span>{showNetworkSection ? 'Hide' : 'Configure'}</span>
                  {showNetworkSection ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
                </div>
              </button>

              {showNetworkSection && (
                <div className="mt-2 p-3 bg-white border border-zinc-200 rounded-md space-y-3 shadow-2xs">
                  <div className="flex items-center justify-between">
                    <span className="text-[11px] text-zinc-600">
                      Region: <code className="text-zinc-900 font-mono">{config.region}</code>
                    </span>
                    <button
                      type="button"
                      onClick={() => fetchNetworks(config.region)}
                      disabled={loadingNetworks}
                      className="flex items-center gap-1 text-[11px] text-zinc-500 hover:text-zinc-950 transition-colors"
                      title="Refresh VPC and Subnet list"
                    >
                      <RefreshCw className={`w-3 h-3 ${loadingNetworks ? 'animate-spin' : ''}`} />
                      <span>Refresh</span>
                    </button>
                  </div>

                  {loadingNetworks ? (
                    <div className="text-xs text-zinc-500 py-3 text-center">Querying AWS VPCs...</div>
                  ) : networkError ? (
                    <div className="p-2 rounded bg-zinc-50 border border-zinc-200 text-xs text-zinc-600">
                      {networkError} (Default VPC used automatically)
                    </div>
                  ) : (
                    <>
                      <div>
                        <label className="block text-[11px] font-medium text-zinc-700 mb-1">
                          Virtual Private Cloud (VPC)
                        </label>
                        <select
                          value={config.vpc_id || ''}
                          onChange={(e) => {
                            const val = e.target.value
                            updateField('vpc_id', val || undefined)
                            updateField('subnet_id', undefined)
                          }}
                          className="w-full bg-white border border-zinc-300 text-zinc-900 rounded-md px-2.5 py-1.5 text-xs font-mono focus:border-zinc-950 focus:ring-1 focus:ring-zinc-950 outline-none shadow-2xs"
                        >
                          <option value="">Default VPC (Auto-detect by Packer)</option>
                          {networks?.vpcs.map((vpc) => (
                            <option key={vpc.vpc_id} value={vpc.vpc_id}>
                              {vpc.name} ({vpc.vpc_id}) — {vpc.cidr}
                            </option>
                          ))}
                        </select>
                      </div>

                      <div>
                        <label className="block text-[11px] font-medium text-zinc-700 mb-1">
                          Subnet (Optional)
                        </label>
                        <select
                          value={config.subnet_id || ''}
                          onChange={(e) => updateField('subnet_id', e.target.value || undefined)}
                          className="w-full bg-white border border-zinc-300 text-zinc-900 rounded-md px-2.5 py-1.5 text-xs font-mono focus:border-zinc-950 focus:ring-1 focus:ring-zinc-950 outline-none shadow-2xs"
                        >
                          <option value="">Auto-assign Subnet in VPC</option>
                          {(networks?.subnets || [])
                            .filter((s) => !config.vpc_id || s.vpc_id === config.vpc_id)
                            .map((s) => (
                              <option key={s.subnet_id} value={s.subnet_id}>
                                {s.name} ({s.subnet_id}) — {s.availability_zone}
                              </option>
                            ))}
                        </select>
                      </div>
                    </>
                  )}
                </div>
              )}
            </div>

            {/* Stepper Navigation Buttons */}
            <div className="flex gap-2 pt-2">
              <button
                type="button"
                onClick={() => setCurrentStep(1)}
                className="py-2.5 px-4 rounded-md text-xs font-medium border border-zinc-300 bg-white text-zinc-800 hover:bg-zinc-50 transition-colors flex items-center justify-center gap-1.5 shadow-2xs"
              >
                <ArrowLeft className="w-3.5 h-3.5" />
                <span>Back</span>
              </button>
              <button
                type="button"
                onClick={() => setCurrentStep(3)}
                className="flex-1 py-2.5 px-4 rounded-md text-xs font-medium bg-zinc-950 text-white hover:bg-zinc-800 transition-colors flex items-center justify-center gap-2 shadow-2xs"
              >
                <span>Next: Review & Build</span>
                <ArrowRight className="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        )}

        {/* ========================================================================= */}
        {/* STAGE 3: Review & build                                                   */}
        {/* ========================================================================= */}
        {currentStep === 3 && (
          <div className="space-y-5">
            <div>
              <h3 className="text-xs font-bold uppercase tracking-wider text-zinc-950 flex items-center gap-1.5 mb-1">
                <CheckCircle className="w-3.5 h-3.5 text-zinc-950" />
                Review & Launch Build
              </h3>
              <p className="text-[11px] text-zinc-500">
                Verify all image specifications before Packer provisions the EC2 build instance.
              </p>
            </div>

            {/* AWS Console Style Summary Card */}
            <div className="p-4 bg-white border border-zinc-200 rounded-lg space-y-3 shadow-2xs font-mono text-xs">
              <div className="border-b border-zinc-100 pb-2 flex items-center justify-between font-sans">
                <span className="font-semibold text-zinc-950 text-xs">Specification Summary</span>
                <span className="text-[11px] text-zinc-500 font-mono">{config.region}</span>
              </div>

              <div className="grid grid-cols-2 gap-3 text-xs">
                <div>
                  <span className="text-zinc-500 text-[10px] uppercase font-sans block">Target AMI Name</span>
                  <span className="font-semibold text-zinc-950">{config.ami_name}</span>
                </div>
                <div>
                  <span className="text-zinc-500 text-[10px] uppercase font-sans block">Architecture</span>
                  <span className="text-zinc-900">{architecture}</span>
                </div>
                <div>
                  <span className="text-zinc-500 text-[10px] uppercase font-sans block">Base AMI ID</span>
                  <span className="text-zinc-900 break-all">{config.source_ami || 'Auto-detect'}</span>
                </div>
                <div>
                  <span className="text-zinc-500 text-[10px] uppercase font-sans block">AMI Owner ID</span>
                  <span className="text-zinc-900">{currentOwnerID || 'Vendor Account'}</span>
                </div>
                <div>
                  <span className="text-zinc-500 text-[10px] uppercase font-sans block">Instance Type</span>
                  <span className="text-zinc-900">{config.instance_type}</span>
                </div>
                <div>
                  <span className="text-zinc-500 text-[10px] uppercase font-sans block">SSH Login User</span>
                  <span className="text-zinc-900">{config.ssh_username}</span>
                </div>
                <div>
                  <span className="text-zinc-500 text-[10px] uppercase font-sans block">Network Target</span>
                  <span className="text-zinc-900">{config.vpc_id ? 'Custom VPC' : 'Default VPC'}</span>
                </div>
                <div>
                  <span className="text-zinc-500 text-[10px] uppercase font-sans block">Installed Packages</span>
                  <span className="text-zinc-900">{config.packages.length} selected</span>
                </div>
              </div>

              {/* Package Pills in Summary */}
              {config.packages.length > 0 && (
                <div className="pt-2 border-t border-zinc-100 flex flex-wrap gap-1">
                  {config.packages.map((p) => (
                    <span
                      key={p}
                      className="bg-zinc-50 border border-zinc-200 text-zinc-800 px-2 py-0.5 rounded text-[10px]"
                    >
                      {p}
                    </span>
                  ))}
                </div>
              )}
            </div>

            {/* Stage 3 Action Buttons */}
            <div className="space-y-2 pt-2">
              {onBuild && (
                <button
                  type="button"
                  onClick={onBuild}
                  disabled={isBuilding}
                  className="w-full py-2.5 px-4 rounded-md text-xs font-semibold bg-zinc-950 text-white hover:bg-zinc-800 disabled:opacity-50 transition-colors flex items-center justify-center gap-2 shadow-2xs"
                >
                  <Play className="w-3.5 h-3.5 fill-current" />
                  <span>{isBuilding ? 'Build in Progress...' : 'Build Machine Image (AMI)'}</span>
                </button>
              )}

              <div className="grid grid-cols-2 gap-2">
                {onPreflight && (
                  <button
                    type="button"
                    onClick={onPreflight}
                    disabled={isBuilding}
                    className="py-2 px-3 rounded-md text-xs font-medium border border-zinc-300 bg-white text-zinc-800 hover:bg-zinc-50 transition-colors flex items-center justify-center gap-1.5 shadow-2xs"
                  >
                    <ShieldCheck className="w-3.5 h-3.5 text-zinc-600" />
                    <span>Pre-Flight Check</span>
                  </button>
                )}
                {onValidate && (
                  <button
                    type="button"
                    onClick={onValidate}
                    disabled={isBuilding}
                    className="py-2 px-3 rounded-md text-xs font-medium border border-zinc-300 bg-white text-zinc-800 hover:bg-zinc-50 transition-colors flex items-center justify-center gap-1.5 shadow-2xs"
                  >
                    <Settings className="w-3.5 h-3.5 text-zinc-600" />
                    <span>Validate HCL</span>
                  </button>
                )}
              </div>

              <button
                type="button"
                onClick={() => setCurrentStep(2)}
                className="w-full py-2 px-4 rounded-md text-xs font-medium border border-zinc-200 bg-zinc-50 text-zinc-600 hover:text-zinc-950 hover:bg-zinc-100 transition-colors flex items-center justify-center gap-1.5"
              >
                <ArrowLeft className="w-3.5 h-3.5" />
                <span>Back to Config & Software</span>
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

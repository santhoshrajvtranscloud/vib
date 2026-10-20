import React, { useState } from 'react'
import {
  Globe,
  Cpu,
  Key,
  Terminal,
  Package,
  Plus,
  X,
  Code2,
  ChevronDown,
  ChevronUp,
  Info,
  Layers,
  Loader2,
  Edit3,
} from 'lucide-react'
import type { BuildConfig, CataloguePackage, BaseAMI } from '../types'

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
}

const REGION_OPTIONS = [
  { value: 'ap-south-1', label: 'Asia Pacific (Mumbai) — ap-south-1' },
  { value: 'us-east-1', label: 'US East (N. Virginia) — us-east-1' },
  { value: 'us-west-2', label: 'US West (Oregon) — us-west-2' },
  { value: 'eu-west-1', label: 'Europe (Ireland) — eu-west-1' },
  { value: 'eu-central-1', label: 'Europe (Frankfurt) — eu-central-1' },
  { value: 'ap-southeast-1', label: 'Asia Pacific (Singapore) — ap-southeast-1' },
]

const INSTANCE_OPTIONS = [
  { value: 't3.micro', label: 't3.micro (2 vCPU, 1 GiB RAM) — Free Tier Eligible' },
  { value: 't3.small', label: 't3.small (2 vCPU, 2 GiB RAM)' },
  { value: 't3.medium', label: 't3.medium (2 vCPU, 4 GiB RAM)' },
  { value: 't3.large', label: 't3.large (2 vCPU, 8 GiB RAM)' },
  { value: 'c5.large', label: 'c5.large (2 vCPU, 4 GiB RAM) — Compute Optimized' },
  { value: 'm5.large', label: 'm5.large (2 vCPU, 8 GiB RAM) — General Purpose' },
  { value: 't4g.micro', label: 't4g.micro (2 vCPU, 1 GiB RAM) — AWS Graviton (arm64)' },
  { value: 't4g.small', label: 't4g.small (2 vCPU, 2 GiB RAM) — AWS Graviton (arm64)' },
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
}) => {
  const [newCustomPkg, setNewCustomPkg] = useState('')
  const [expandedPkg, setExpandedPkg] = useState<string | null>(null)
  const [customScriptText, setCustomScriptText] = useState(
    (config.custom_script || []).join('\n')
  )
  const [showManualAMI, setShowManualAMI] = useState(false)

  const updateField = <K extends keyof BuildConfig>(key: K, value: BuildConfig[K]) => {
    onChange({
      ...config,
      [key]: value,
    })
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

  const selectBaseAMI = (ami: BaseAMI) => {
    onChange({
      ...config,
      source_ami: ami.id,
      ssh_username: ami.default_user,
    })
  }

  return (
    <div className="flex flex-col h-full bg-zinc-950 border-r border-zinc-800 overflow-y-auto">
      {/* Panel Title & Manual Edit Alert */}
      <div className="p-4 border-b border-zinc-800 bg-zinc-950 sticky top-0 z-10">
        <div className="flex items-center justify-between">
          <h2 className="text-xs font-bold text-white tracking-wider uppercase">
            Builder Configuration
          </h2>
          {isManualMode ? (
            <button
              onClick={onResetToForm}
              className="text-xs border border-zinc-700 bg-zinc-900 text-zinc-300 hover:border-zinc-400 hover:text-white px-2.5 py-1 rounded-md transition-colors"
            >
              Reset to UI
            </button>
          ) : (
            <span className="text-[11px] font-mono text-zinc-400 border border-zinc-800 bg-zinc-900 px-2 py-0.5 rounded-md">
              Live Sync
            </span>
          )}
        </div>
        {isManualMode && (
          <div className="mt-2.5 text-xs text-zinc-300 bg-zinc-900 border border-zinc-800 rounded-md p-2.5 flex items-start gap-2">
            <Info className="w-3.5 h-3.5 shrink-0 text-zinc-400 mt-0.5" />
            <span>
              Manual HCL editing active. Changes made in this form will not overwrite the editor until you click &quot;Reset to UI&quot;.
            </span>
          </div>
        )}
      </div>

      <div className="p-4 space-y-6">
        {/* Section 1: Target AMI & Region */}
        <div className="space-y-4">
          <h3 className="text-xs font-semibold text-zinc-400 uppercase tracking-wider flex items-center gap-1.5">
            <Globe className="w-3.5 h-3.5 text-zinc-400" />
            Destination Specs
          </h3>

          {/* AMI Name */}
          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1">
              Target AMI Name <span className="text-white">*</span>
            </label>
            <input
              type="text"
              value={config.ami_name}
              onChange={(e) => updateField('ami_name', e.target.value)}
              placeholder="e.g. vib-ubuntu-nginx-app"
              className="w-full bg-zinc-900 border border-zinc-800 text-white focus:border-white focus:ring-0 outline-none rounded-md px-3 py-2 text-xs font-mono placeholder:text-zinc-600 transition-colors"
            />
            <p className="mt-1 text-[11px] text-zinc-500">
              VIB automatically appends <code className="text-zinc-400">-&#123;&#123;timestamp&#125;&#125;</code> to guarantee uniqueness in AWS.
            </p>
          </div>

          {/* Region */}
          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1">
              AWS Region <span className="text-white">*</span>
            </label>
            <select
              value={config.region}
              onChange={(e) => updateField('region', e.target.value)}
              className="w-full bg-zinc-900 border border-zinc-800 text-white focus:border-white focus:ring-0 outline-none rounded-md px-3 py-2 text-xs transition-colors"
            >
              {REGION_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value} className="bg-zinc-900 text-white">
                  {opt.label}
                </option>
              ))}
            </select>
          </div>

          {/* Instance Type */}
          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1 flex items-center gap-1">
              <Cpu className="w-3.5 h-3.5 text-zinc-400" />
              Build Instance Type <span className="text-white">*</span>
            </label>
            <select
              value={config.instance_type}
              onChange={(e) => updateField('instance_type', e.target.value)}
              className="w-full bg-zinc-900 border border-zinc-800 text-white focus:border-white focus:ring-0 outline-none rounded-md px-3 py-2 text-xs font-mono transition-colors"
            >
              {INSTANCE_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value} className="bg-zinc-900 text-white">
                  {opt.label}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* Section 2: Official Base Image Discovery */}
        <div className="space-y-3 pt-4 border-t border-zinc-800">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-semibold text-zinc-400 uppercase tracking-wider flex items-center gap-1.5">
              <Layers className="w-3.5 h-3.5 text-zinc-400" />
              Official Base Image
            </h3>

            {/* Architecture Toggle */}
            <div className="flex items-center bg-zinc-900 rounded-md p-0.5 border border-zinc-800 text-[11px]">
              <button
                type="button"
                onClick={() => onArchChange('x86_64')}
                className={`px-2 py-0.5 rounded font-mono transition-colors ${
                  architecture === 'x86_64'
                    ? 'bg-white text-black font-medium'
                    : 'text-zinc-400 hover:text-white'
                }`}
              >
                x86_64
              </button>
              <button
                type="button"
                onClick={() => onArchChange('arm64')}
                className={`px-2 py-0.5 rounded font-mono transition-colors ${
                  architecture === 'arm64'
                    ? 'bg-white text-black font-medium'
                    : 'text-zinc-400 hover:text-white'
                }`}
              >
                arm64
              </button>
            </div>
          </div>

          <div className="flex items-center justify-between text-[11px]">
            <span className="text-zinc-400">
              Verified distributions for <code className="text-zinc-200">{config.region}</code>:
            </span>
            {baseAMIsLoading ? (
              <span className="flex items-center gap-1 text-zinc-400 text-[10px]">
                <Loader2 className="w-3 h-3 animate-spin text-zinc-400" />
                Querying EC2...
              </span>
            ) : (
              <span className="text-[10px] font-mono text-zinc-400 border border-zinc-800 bg-zinc-900 px-1.5 py-0.2 rounded">
                {baseAMISource === 'aws_live' ? 'Live EC2' : 'Curated Preset'}
              </span>
            )}
          </div>

          {/* Base AMI Cards */}
          <div className="space-y-2">
            {baseAMIs.map((ami) => {
              const isSelected = config.source_ami === ami.id

              return (
                <div
                  key={ami.id + ami.os}
                  onClick={() => selectBaseAMI(ami)}
                  className={`p-3 rounded-md border cursor-pointer transition-all ${
                    isSelected
                      ? 'border-white bg-zinc-900 text-white'
                      : 'border-zinc-800 bg-zinc-950 text-zinc-400 hover:border-zinc-700 hover:text-zinc-300'
                  }`}
                >
                  <div className="flex items-start justify-between">
                    <div className="flex items-start gap-2.5">
                      <div
                        className={`mt-0.5 w-3.5 h-3.5 rounded-full border flex items-center justify-center shrink-0 transition-colors ${
                          isSelected ? 'border-white bg-white text-black' : 'border-zinc-700 bg-zinc-950'
                        }`}
                      >
                        {isSelected && <div className="w-1.5 h-1.5 rounded-full bg-black"></div>}
                      </div>
                      <div>
                        <div className="text-xs font-medium flex items-center gap-2">
                          <span className={isSelected ? 'text-white' : 'text-zinc-200'}>{ami.display_name}</span>
                          <span className="text-[10px] font-mono text-zinc-400 border border-zinc-800 bg-zinc-950 px-1.5 py-0.2 rounded">
                            user: {ami.default_user}
                          </span>
                        </div>
                        <p className="text-[11px] text-zinc-400 mt-0.5 line-clamp-1 leading-relaxed">
                          {ami.description}
                        </p>
                      </div>
                    </div>
                  </div>

                  <div className="mt-2 pt-2 border-t border-zinc-800/80 flex items-center justify-between text-[10px] font-mono text-zinc-500">
                    <span className={isSelected ? 'text-zinc-300' : 'text-zinc-500'}>{ami.id}</span>
                    <span>{ami.architecture}</span>
                  </div>
                </div>
              )
            })}
          </div>

          {/* Toggle or manual entry */}
          <div className="pt-1">
            <button
              type="button"
              onClick={() => setShowManualAMI(!showManualAMI)}
              className="text-[11px] text-zinc-400 hover:text-zinc-200 flex items-center gap-1 transition-colors"
            >
              <Edit3 className="w-3 h-3" />
              <span>{showManualAMI ? 'Hide Custom AMI Input' : 'Enter Custom / Private AMI ID'}</span>
            </button>

            {showManualAMI && (
              <div className="mt-2 space-y-2 p-3 bg-zinc-900 border border-zinc-800 rounded-md">
                <div>
                  <label className="block text-[11px] font-medium text-zinc-300 mb-1">
                    Custom Source AMI ID
                  </label>
                  <input
                    type="text"
                    value={config.source_ami}
                    onChange={(e) => updateField('source_ami', e.target.value)}
                    placeholder="ami-0123456789abcdef0"
                    className="w-full bg-zinc-950 border border-zinc-800 rounded-md px-2.5 py-1.5 text-xs text-white placeholder:text-zinc-600 focus:border-white focus:ring-0 outline-none font-mono"
                  />
                </div>
              </div>
            )}
          </div>

          {/* SSH Username */}
          <div className="pt-1">
            <label className="block text-xs font-medium text-zinc-300 mb-1 flex items-center gap-1">
              <Key className="w-3.5 h-3.5 text-zinc-400" />
              SSH Username (Source OS Login)
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                value={config.ssh_username}
                onChange={(e) => updateField('ssh_username', e.target.value)}
                placeholder="ubuntu"
                className="flex-1 bg-zinc-900 border border-zinc-800 rounded-md px-3 py-1.5 text-xs text-white placeholder:text-zinc-600 focus:border-white focus:ring-0 outline-none font-mono"
              />
              {(['ubuntu', 'ec2-user', 'admin'] as const).map((user) => (
                <button
                  key={user}
                  type="button"
                  onClick={() => updateField('ssh_username', user)}
                  className={`text-xs px-2.5 py-1 rounded-md border transition-colors ${
                    config.ssh_username === user
                      ? 'bg-white text-black font-medium border-white'
                      : 'border-zinc-800 bg-zinc-900 text-zinc-400 hover:border-zinc-700 hover:text-white'
                  }`}
                >
                  {user}
                </button>
              ))}
            </div>
          </div>
        </div>

        {/* Section 3: Software Catalogue */}
        <div className="space-y-4 pt-4 border-t border-zinc-800">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-semibold text-zinc-400 uppercase tracking-wider flex items-center gap-1.5">
              <Package className="w-3.5 h-3.5 text-zinc-400" />
              Software Packages
            </h3>
            <span className="text-[11px] text-zinc-500 font-mono">
              {config.packages.length} selected
            </span>
          </div>

          {catalogueLoading ? (
            <div className="text-xs text-zinc-500 py-3 text-center">
              Loading package catalogue...
            </div>
          ) : (
            <div className="space-y-2">
              {catalogue.map((pkg) => {
                const isSelected = config.packages.includes(pkg.id)
                const isExpanded = expandedPkg === pkg.id

                return (
                  <div
                    key={pkg.id}
                    className={`rounded-md border transition-all ${
                      isSelected
                        ? 'border-white bg-zinc-900 text-white'
                        : 'border-zinc-800 bg-zinc-950 text-zinc-400 hover:border-zinc-700 hover:text-zinc-300'
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
                          className="mt-0.5 w-4 h-4 rounded border-zinc-700 bg-zinc-950 text-white accent-white focus:ring-0 cursor-pointer"
                        />
                        <div>
                          <div className="text-xs font-medium flex items-center gap-2">
                            <span className={isSelected ? 'text-white' : 'text-zinc-200'}>{pkg.name}</span>
                            <span className="text-[10px] font-mono text-zinc-500 bg-zinc-900 border border-zinc-800 px-1.5 py-0.2 rounded">
                              {pkg.id}
                            </span>
                          </div>
                          <p className="text-[11px] text-zinc-400 mt-0.5 leading-relaxed">
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
                        className="text-zinc-500 hover:text-zinc-300 p-0.5 rounded"
                        title="View commands"
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
                      <div className="px-3 pb-3 pt-1 border-t border-zinc-800 bg-black/40 rounded-b-md">
                        <div className="text-[10px] uppercase font-mono text-zinc-500 mb-1 flex items-center gap-1">
                          <Code2 className="w-3 h-3 text-zinc-400" />
                          Commands Executed:
                        </div>
                        <pre className="text-[11px] font-mono text-zinc-300 bg-zinc-950 p-2 rounded border border-zinc-800 overflow-x-auto">
                          {pkg.commands.join('\n')}
                        </pre>
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )}

          {/* Add custom apt package */}
          <div className="pt-2">
            <label className="block text-xs font-medium text-zinc-400 mb-1">
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
                className="flex-1 bg-zinc-900 border border-zinc-800 rounded-md px-3 py-1.5 text-xs text-white placeholder:text-zinc-600 focus:border-white focus:ring-0 outline-none font-mono"
              />
              <button
                type="button"
                onClick={addCustomPackage}
                className="border border-zinc-700 text-zinc-300 hover:border-zinc-400 hover:text-white bg-transparent px-3 py-1.5 rounded-md text-xs font-medium flex items-center gap-1 transition-colors"
              >
                <Plus className="w-3.5 h-3.5" />
                Add
              </button>
            </div>

            {/* Custom packages list pills */}
            {config.packages.filter(
              (p) => !catalogue.some((cat) => cat.id === p)
            ).length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1.5">
                {config.packages
                  .filter((p) => !catalogue.some((cat) => cat.id === p))
                  .map((customPkg) => (
                    <span
                      key={customPkg}
                      className="inline-flex items-center gap-1 text-[11px] font-mono bg-zinc-900 text-zinc-300 border border-zinc-800 px-2 py-0.5 rounded-md"
                    >
                      {customPkg}
                      <button
                        type="button"
                        onClick={() => togglePackage(customPkg)}
                        className="text-zinc-500 hover:text-white"
                      >
                        <X className="w-3 h-3" />
                      </button>
                    </span>
                  ))}
              </div>
            )}
          </div>
        </div>

        {/* Section 4: Custom Inline Shell Scripts */}
        <div className="space-y-2 pt-4 border-t border-zinc-800">
          <h3 className="text-xs font-semibold text-zinc-400 uppercase tracking-wider flex items-center gap-1.5">
            <Terminal className="w-3.5 h-3.5 text-zinc-400" />
            Custom Shell Commands
          </h3>
          <p className="text-[11px] text-zinc-500 leading-relaxed">
            Commands to execute during the Packer shell provisioner step (one per line).
          </p>
          <textarea
            rows={4}
            value={customScriptText}
            onChange={(e) => handleCustomScriptChange(e.target.value)}
            placeholder={`echo "Setting up application..."\nsudo mkdir -p /var/www/app\nsudo chown -R ubuntu:ubuntu /var/www`}
            className="w-full bg-zinc-900 border border-zinc-800 text-white focus:border-white focus:ring-0 outline-none rounded-md p-3 text-xs font-mono placeholder:text-zinc-600 transition-colors resize-y"
          />
        </div>
      </div>
    </div>
  )
}

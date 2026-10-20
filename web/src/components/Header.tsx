import React from 'react'
import {
  Globe,
  CheckCircle2,
  XCircle,
  Ban,
  ChevronDown,
} from 'lucide-react'
import type { HealthResponse, BuildStatusSummary, AWSIdentityInfo } from '../types'

interface HeaderProps {
  region: string
  onRegionChange: (region: string) => void
  awsStatus: AWSIdentityInfo | null
  onOpenAWSModal: () => void
  health: HealthResponse | null
  healthLoading: boolean
  healthError: boolean
  currentBuild: BuildStatusSummary | null
}

const REGION_OPTIONS = [
  { value: 'ap-south-1', label: 'Asia Pacific (Mumbai) — ap-south-1' },
  { value: 'us-east-1', label: 'US East (N. Virginia) — us-east-1' },
  { value: 'us-west-2', label: 'US West (Oregon) — us-west-2' },
  { value: 'eu-west-1', label: 'Europe (Ireland) — eu-west-1' },
  { value: 'eu-central-1', label: 'Europe (Frankfurt) — eu-central-1' },
  { value: 'ap-southeast-1', label: 'Asia Pacific (Singapore) — ap-southeast-1' },
]

export const Header: React.FC<HeaderProps> = ({
  region,
  onRegionChange,
  awsStatus,
  onOpenAWSModal,
  health,
  healthLoading,
  healthError,
  currentBuild,
}) => {
  const getStatusBadge = () => {
    if (!currentBuild || currentBuild.status === 'idle') {
      return (
        <span className="hidden sm:inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-mono text-zinc-500 border border-zinc-200 bg-zinc-50">
          <span className="w-1.5 h-1.5 rounded-full bg-zinc-400"></span>
          Idle
        </span>
      )
    }

    switch (currentBuild.status) {
      case 'running':
      case 'pending':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded text-[11px] font-mono text-zinc-950 border border-zinc-300 bg-zinc-100 animate-pulse">
            <span className="w-1.5 h-1.5 rounded-full bg-zinc-950"></span>
            Building AMI...
          </span>
        )
      case 'success':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded text-[11px] font-mono text-zinc-950 border border-zinc-300 bg-zinc-50">
            <CheckCircle2 className="w-3.5 h-3.5 text-zinc-950" />
            Build Succeeded
          </span>
        )
      case 'failed':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded text-[11px] font-mono text-zinc-800 border border-zinc-300 bg-zinc-100">
            <XCircle className="w-3.5 h-3.5 text-zinc-600" />
            Build Failed
          </span>
        )
      case 'cancelling':
      case 'cancelled':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded text-[11px] font-mono text-zinc-500 border border-zinc-200 bg-zinc-50">
            <Ban className="w-3.5 h-3.5 text-zinc-400" />
            {currentBuild.status === 'cancelling' ? 'Cancelling...' : 'Cancelled'}
          </span>
        )
      default:
        return null
    }
  }

  return (
    <header className="h-12 border-b border-zinc-200 bg-white px-4 flex items-center justify-between shrink-0 z-20 select-none">
      {/* Left: Branding & Status */}
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-2">
          <span className="text-sm font-bold tracking-tight text-zinc-950">VIB</span>
          <span className="text-zinc-300 text-sm">/</span>
          <span className="text-xs font-medium text-zinc-600 tracking-tight">
            AWS Machine Image Builder
          </span>
        </div>

        {/* Live Build Status Pill */}
        <div className="hidden sm:flex items-center">
          {getStatusBadge()}
        </div>
      </div>

      {/* Right: Global Region Selector, AWS Connection Pill, and Backend Health */}
      <div className="flex items-center gap-2.5">
        {/* Global AWS Region Dropdown */}
        <div className="relative flex items-center">
          <Globe className="w-3.5 h-3.5 text-zinc-500 absolute left-2.5 pointer-events-none" />
          <select
            value={region}
            onChange={(e) => onRegionChange(e.target.value)}
            className="appearance-none bg-zinc-50 hover:bg-zinc-100 border border-zinc-200 text-zinc-900 text-xs font-mono rounded-md pl-8 pr-7 py-1 outline-none focus:border-zinc-950 cursor-pointer transition-colors shadow-2xs"
            title="Active AWS Region"
          >
            {REGION_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
          <ChevronDown className="w-3 h-3 text-zinc-400 absolute right-2 pointer-events-none" />
        </div>

        {/* AWS Connection Button / Identity Pill */}
        <button
          type="button"
          onClick={onOpenAWSModal}
          className={`flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs border transition-colors shadow-2xs ${
            awsStatus?.connected
              ? 'border-zinc-300 bg-white text-zinc-900 hover:bg-zinc-50 hover:border-zinc-400'
              : 'border-zinc-200 bg-zinc-50 text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900'
          }`}
          title="Configure AWS Credentials and STS Identity"
        >
          {awsStatus?.connected ? (
            <>
              <span className="text-[10px] text-zinc-950">●</span>
              <span className="font-medium font-mono text-[11px]">
                Connected: {awsStatus.account_id ? awsStatus.account_id : 'AWS'}
              </span>
            </>
          ) : (
            <>
              <span className="text-[10px] text-zinc-400">○</span>
              <span className="font-medium text-[11px]">Disconnected</span>
            </>
          )}
        </button>

        {/* Backend Health Check */}
        <div className="hidden lg:flex items-center gap-1.5 text-[11px] text-zinc-600 border border-zinc-200 bg-zinc-50 px-2 py-1 rounded-md font-mono">
          <span className={`w-1.5 h-1.5 rounded-full ${healthError ? 'bg-zinc-400' : 'bg-zinc-950'}`}></span>
          <span>
            {healthLoading
              ? 'checking...'
              : healthError
              ? 'Offline'
              : health?.packer_ver
              ? `Packer ${health.packer_ver}`
              : 'Ready'}
          </span>
        </div>
      </div>
    </header>
  )
}

import React from 'react'
import {
  CheckCircle2,
  XCircle,
  Ban,
} from 'lucide-react'
import type { HealthResponse, BuildStatusSummary, AWSIdentityInfo } from '../types'

interface HeaderProps {
  health: HealthResponse | null
  healthLoading: boolean
  healthError: boolean
  currentBuild: BuildStatusSummary | null
  awsStatus: AWSIdentityInfo | null
  onOpenAWSModal: () => void
  activeTab: 'builder' | 'amis'
  onTabChange: (tab: 'builder' | 'amis') => void
}

export const Header: React.FC<HeaderProps> = ({
  health,
  healthLoading,
  healthError,
  currentBuild,
  awsStatus,
  onOpenAWSModal,
  activeTab,
  onTabChange,
}) => {
  const getStatusBadge = () => {
    if (!currentBuild || currentBuild.status === 'idle') {
      return (
        <span className="hidden sm:inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-mono text-zinc-500 border border-zinc-200 bg-zinc-50">
          <span className="w-1.5 h-1.5 rounded-full bg-zinc-400"></span>
          Idle
        </span>
      )
    }

    switch (currentBuild.status) {
      case 'running':
      case 'pending':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-mono text-zinc-950 border border-zinc-300 bg-zinc-100 animate-pulse">
            <span className="w-1.5 h-1.5 rounded-full bg-zinc-950"></span>
            Building AMI...
          </span>
        )
      case 'success':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-mono text-zinc-950 border border-zinc-300 bg-zinc-50">
            <CheckCircle2 className="w-3.5 h-3.5 text-zinc-950" />
            Build Succeeded
          </span>
        )
      case 'failed':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-mono text-zinc-800 border border-zinc-300 bg-zinc-100">
            <XCircle className="w-3.5 h-3.5 text-zinc-600" />
            Build Failed
          </span>
        )
      case 'cancelling':
      case 'cancelled':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-mono text-zinc-500 border border-zinc-200 bg-zinc-50">
            <Ban className="w-3.5 h-3.5 text-zinc-400" />
            {currentBuild.status === 'cancelling' ? 'Cancelling...' : 'Cancelled'}
          </span>
        )
      default:
        return null
    }
  }

  return (
    <header className="h-13 border-b border-zinc-200 bg-white px-4 flex items-center justify-between shrink-0 z-20 select-none">
      {/* Left: Branding & Navigation Switcher */}
      <div className="flex items-center gap-5">
        <div className="flex items-baseline gap-2.5">
          <span className="text-base font-bold tracking-tight text-zinc-950">VIB</span>
          <span className="text-xs text-zinc-500 hidden md:inline">
            Visual Image Builder
          </span>
        </div>

        {/* Tab Navigation Switcher: [ Builder ] | [ AMIs & History ] */}
        <div className="flex items-center bg-zinc-100 border border-zinc-200 rounded-md p-0.5 text-xs font-mono">
          <button
            type="button"
            onClick={() => onTabChange('builder')}
            className={`px-3 py-1 rounded transition-colors ${
              activeTab === 'builder'
                ? 'bg-white text-zinc-950 font-medium shadow-2xs'
                : 'text-zinc-600 hover:text-zinc-950'
            }`}
          >
            Builder
          </button>
          <button
            type="button"
            onClick={() => onTabChange('amis')}
            className={`px-3 py-1 rounded transition-colors ${
              activeTab === 'amis'
                ? 'bg-white text-zinc-950 font-medium shadow-2xs'
                : 'text-zinc-600 hover:text-zinc-950'
            }`}
          >
            AMIs & History
          </button>
        </div>
      </div>

      {/* Middle: Build Status Pill */}
      <div className="flex items-center gap-3">
        {getStatusBadge()}
      </div>

      {/* Right: AWS Connection Status & Backend Health */}
      <div className="flex items-center gap-2.5 text-xs">
        {/* Discreet AWS Connection Pill */}
        <button
          type="button"
          onClick={onOpenAWSModal}
          className={`flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs border transition-colors ${
            awsStatus?.connected
              ? 'border-zinc-300 bg-white text-zinc-900 hover:bg-zinc-50 hover:border-zinc-400 shadow-2xs'
              : 'border-zinc-200 bg-zinc-50 text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900'
          }`}
          title="Configure AWS Credentials and STS Identity"
        >
          {awsStatus?.connected ? (
            <>
              <span className="text-[10px] text-zinc-950">●</span>
              <span className="font-medium">
                {awsStatus.account_id ? `Connected (${awsStatus.account_id.slice(-4)})` : 'Connected'}
              </span>
            </>
          ) : (
            <>
              <span className="text-[10px] text-zinc-400">○</span>
              <span className="font-medium">Disconnected</span>
            </>
          )}
        </button>

        {/* Backend Health Check */}
        <div className="hidden md:flex items-center gap-1.5 text-xs text-zinc-600 border border-zinc-200 bg-zinc-50 px-2.5 py-1 rounded-md font-mono">
          <span className={`w-1.5 h-1.5 rounded-full ${healthError ? 'bg-zinc-400' : 'bg-zinc-950'}`}></span>
          <span>{healthLoading ? 'checking...' : healthError ? 'Offline' : 'Ready'}</span>
        </div>

        {/* Packer CLI Version */}
        {health?.packer_ver && (
          <div className="hidden lg:flex items-center gap-1 text-xs text-zinc-600 font-mono border border-zinc-200 bg-zinc-50 px-2.5 py-1 rounded-md">
            <span>Packer {health.packer_ver}</span>
          </div>
        )}
      </div>
    </header>
  )
}

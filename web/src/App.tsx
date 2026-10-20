import { useState, useEffect, useRef, useCallback } from 'react'
import { Header } from './components/Header'
import { VisualBuilder } from './components/VisualBuilder'
import { HclEditor } from './components/HclEditor'
import { TerminalLogs } from './components/TerminalLogs'
import { AWSModal } from './components/AWSModal'
import { AmiDashboard } from './components/AmiDashboard'
import { apiFetch } from './api'
import type {
  BuildConfig,
  CataloguePackage,
  HealthResponse,
  BuildStatusSummary,
  LogEntry,
  HCLValidateResponse,
  AWSIdentityInfo,
  BaseAMI,
  BaseAMIResponse,
  PreflightReport,
} from './types'

const DEFAULT_CONFIG: BuildConfig = {
  ami_name: 'vib-ubuntu-app',
  region: 'ap-south-1',
  instance_type: 't3.micro',
  source_ami: 'ami-0f58b397bc5c1f2e8',
  ssh_username: 'ubuntu',
  packages: ['nginx', 'docker'],
  custom_script: [],
}

export function App() {
  const [config, setConfig] = useState<BuildConfig>(DEFAULT_CONFIG)
  const [hclCode, setHclCode] = useState<string>('')
  const [isManualMode, setIsManualMode] = useState(false)
  const [catalogue, setCatalogue] = useState<CataloguePackage[]>([])
  const [catalogueLoading, setCatalogueLoading] = useState(true)
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [healthLoading, setHealthLoading] = useState(true)
  const [healthError, setHealthError] = useState(false)
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [currentBuild, setCurrentBuild] = useState<BuildStatusSummary | null>(null)
  const [isValidating, setIsValidating] = useState(false)
  const [isPreflighting, setIsPreflighting] = useState(false)

  // AWS Integration States
  const [awsStatus, setAwsStatus] = useState<AWSIdentityInfo | null>(null)
  const [isAWSModalOpen, setIsAWSModalOpen] = useState(false)
  const [baseAMIs, setBaseAMIs] = useState<BaseAMI[]>([])
  const [baseAMIsLoading, setBaseAMIsLoading] = useState(true)
  const [baseAMISource, setBaseAMISource] = useState<string>('fallback_preset')
  const [architecture, setArchitecture] = useState<'x86_64' | 'arm64'>('x86_64')
  const [activeTab, setActiveTab] = useState<'builder' | 'amis'>('builder')
  const [isSyncingToForm, setIsSyncingToForm] = useState(false)
  const [syncNotification, setSyncNotification] = useState<{
    type: 'success' | 'error'
    message: string
  } | null>(null)

  const activeEventSourceRef = useRef<EventSource | null>(null)
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Connect to SSE stream
  const connectSSE = useCallback((buildId: string) => {
    if (activeEventSourceRef.current) {
      activeEventSourceRef.current.close()
    }

    const es = new EventSource(`/api/builds/${buildId}/stream`)
    activeEventSourceRef.current = es

    es.addEventListener('log', (event: MessageEvent) => {
      try {
        const entry: LogEntry = JSON.parse(event.data)
        setLogs((prev) => [...prev, entry])
      } catch (err) {
        console.error('Failed to parse SSE log event:', err)
      }
    })

    es.addEventListener('status', (event: MessageEvent) => {
      try {
        const statusSummary: BuildStatusSummary = JSON.parse(event.data)
        setCurrentBuild(statusSummary)
        es.close()
      } catch (err) {
        console.error('Failed to parse SSE status event:', err)
      }
    })

    es.onerror = () => {
      es.close()
    }
  }, [])

  // 1. Fetch Health, Catalogue, and AWS Status on Mount
  const fetchAWSStatus = useCallback(async () => {
    try {
      const res = await apiFetch(`/api/aws/status?region=${config.region}`)
      if (res.ok) {
        const data: AWSIdentityInfo = await res.json()
        setAwsStatus(data)
      }
    } catch (err) {
      console.warn('AWS status check notice:', err)
    }
  }, [config.region])

  useEffect(() => {
    const fetchHealth = async () => {
      try {
        setHealthLoading(true)
        const res = await apiFetch('/api/health')
        if (!res.ok) throw new Error('Health check failed')
        const data = await res.json()
        setHealth(data)
        setHealthError(false)
      } catch {
        setHealthError(true)
      } finally {
        setHealthLoading(false)
      }
    }

    const fetchCatalogue = async () => {
      try {
        setCatalogueLoading(true)
        const res = await apiFetch('/api/catalogue')
        if (!res.ok) throw new Error('Failed to fetch catalogue')
        const data = await res.json()
        setCatalogue(data.packages || [])
      } catch (err) {
        console.error('Failed to load catalogue:', err)
      } finally {
        setCatalogueLoading(false)
      }
    }

    // Check if any build is currently running and reconnect SSE stream
    const checkRunningBuilds = async () => {
      try {
        const res = await apiFetch('/api/builds')
        if (res.ok) {
          const data = await res.json()
          const builds: BuildStatusSummary[] = data.builds || []
          const running = builds.find(
            (b) => b.status === 'running' || b.status === 'pending'
          )
          if (running) {
            setCurrentBuild(running)
            connectSSE(running.build_id)
          }
        }
      } catch (err) {
        console.warn('Failed to check running builds on load:', err)
      }
    }

    fetchHealth()
    fetchCatalogue()
    fetchAWSStatus()
    checkRunningBuilds()

    const healthInterval = setInterval(fetchHealth, 30000)
    return () => {
      clearInterval(healthInterval)
      if (activeEventSourceRef.current) {
        activeEventSourceRef.current.close()
      }
    }
  }, [fetchAWSStatus, connectSSE])

  // 2. Fetch Official Base AMIs when Region or Architecture changes
  const fetchBaseAMIs = useCallback(async (region: string, arch: string) => {
    try {
      setBaseAMIsLoading(true)
      const res = await apiFetch(`/api/aws/base-amis?region=${region}&arch=${arch}`)
      if (!res.ok) throw new Error('Failed to fetch base AMIs')
      const data: BaseAMIResponse = await res.json()
      setBaseAMIs(data.images || [])
      setBaseAMISource(data.source)
    } catch (err) {
      console.warn('Base AMI discovery notice:', err)
    } finally {
      setBaseAMIsLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchBaseAMIs(config.region, architecture)
  }, [config.region, architecture, fetchBaseAMIs])

  // 3. Auto-generate HCL on form changes (Debounced)
  const generateHCL = useCallback(async (cfgToUse: BuildConfig) => {
    try {
      const res = await apiFetch('/api/hcl/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(cfgToUse),
      })

      if (!res.ok) {
        const errData = await res.json().catch(() => ({}))
        console.warn('HCL generation notice:', errData.error)
        return
      }

      const data = await res.json()
      if (data.hcl) {
        setHclCode(data.hcl)
      }
    } catch (err) {
      console.error('Error generating HCL:', err)
    }
  }, [])

  useEffect(() => {
    if (isManualMode) return

    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current)
    }

    debounceTimerRef.current = setTimeout(() => {
      generateHCL(config)
    }, 250)

    return () => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current)
      }
    }
  }, [config, isManualMode, generateHCL])

  // Initial generation on component load
  useEffect(() => {
    generateHCL(DEFAULT_CONFIG)
  }, [generateHCL])

  // 4. User manually types in editor
  const handleEditorChange = (value: string) => {
    setHclCode(value)
    setIsManualMode(true)
  }

  // 5. Reset to Form mode
  const handleResetToForm = () => {
    setIsManualMode(false)
    setSyncNotification(null)
    generateHCL(config)
  }

  // 5b. Bi-directional sync: parse HCL back into Form state
  const handleSyncToForm = async () => {
    setIsSyncingToForm(true)
    setSyncNotification(null)
    try {
      const res = await apiFetch('/api/hcl/parse', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hcl: hclCode }),
      })
      const data = await res.json()
      if (!res.ok) {
        throw new Error(data.error || 'Failed to parse HCL configuration')
      }
      if (data.config) {
        setConfig(data.config)
        setIsManualMode(false)
        setSyncNotification({
          type: 'success',
          message: 'HCL parsed successfully & synced to form!',
        })
        setTimeout(() => {
          setSyncNotification((prev) => (prev?.type === 'success' ? null : prev))
        }, 3500)
      }
    } catch (err: any) {
      setSyncNotification({
        type: 'error',
        message: err.message || 'Syntax error: could not parse HCL into form',
      })
    } finally {
      setIsSyncingToForm(false)
    }
  }

  // 6. Validate HCL
  const handleValidate = async () => {
    setIsValidating(true)
    const startMsg: LogEntry = {
      timestamp: new Date().toISOString(),
      stream: 'system',
      message: `==> Triggering 'packer validate' on current HCL...`,
    }
    setLogs((prev) => [...prev, startMsg])

    try {
      const res = await apiFetch('/api/hcl/validate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hcl: hclCode }),
      })

      const data: HCLValidateResponse = await res.json()
      if (data.logs && data.logs.length > 0) {
        setLogs((prev) => [...prev, ...data.logs])
      }

      if (data.valid) {
        setLogs((prev) => [
          ...prev,
          {
            timestamp: new Date().toISOString(),
            stream: 'system',
            message: 'Configuration is valid and ready to build.',
          },
        ])
      } else {
        setLogs((prev) => [
          ...prev,
          {
            timestamp: new Date().toISOString(),
            stream: 'stderr',
            message: `Validation failed (${data.stage}): ${data.error}`,
          },
        ])
      }
    } catch (err) {
      setLogs((prev) => [
        ...prev,
        {
          timestamp: new Date().toISOString(),
          stream: 'stderr',
          message: `Network error during validation: ${err}`,
        },
      ])
    } finally {
      setIsValidating(false)
    }
  }

  // 6b. Run AWS Pre-Flight Readiness Check
  const handlePreflightCheck = async () => {
    setIsPreflighting(true)
    setActiveTab('builder')

    const startMsg: LogEntry = {
      timestamp: new Date().toISOString(),
      stream: 'system',
      message: `==> [Pre-Flight Diagnostic] Running 4-step AWS readiness check for region '${config.region}'...`,
    }
    setLogs((prev) => [...prev, startMsg])

    try {
      const res = await apiFetch('/api/aws/preflight', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          region: config.region,
          source_ami: config.source_ami,
          instance_type: config.instance_type,
        }),
      })

      if (!res.ok) {
        throw new Error(`Pre-flight diagnostic failed with status ${res.status}`)
      }

      const report: PreflightReport = await res.json()
      const checkLogs: LogEntry[] = report.checks.map((chk) => ({
        timestamp: new Date().toISOString(),
        stream: chk.passed ? 'stdout' : 'stderr',
        message: `[${chk.passed ? '✓' : '✗'}] ${chk.title}: ${chk.message}`,
      }))

      const conclusionLog: LogEntry = {
        timestamp: new Date().toISOString(),
        stream: 'system',
        message: report.passed
          ? '==> [Pre-Flight Diagnostic] AWS infrastructure is 100% READY for building AMIs!'
          : '==> [Pre-Flight Diagnostic] Action needed: Resolve the failed checks above before building.',
      }

      setLogs((prev) => [...prev, ...checkLogs, conclusionLog])
    } catch (err: any) {
      setLogs((prev) => [
        ...prev,
        {
          timestamp: new Date().toISOString(),
          stream: 'stderr',
          message: `[✗] Pre-Flight Diagnostic error: ${err.message || 'Unknown network error'}`,
        },
      ])
    } finally {
      setIsPreflighting(false)
    }
  }

  // 7. Start Build
  const handleBuild = async () => {
    setLogs([
      {
        timestamp: new Date().toISOString(),
        stream: 'system',
        message: '==> Initiating Packer build job with injected AWS credentials...',
      },
    ])

    const payload = isManualMode ? { hcl: hclCode } : { config }

    try {
      const res = await apiFetch('/api/builds', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })

      if (!res.ok) {
        const err = await res.json().catch(() => ({}))
        setLogs((prev) => [
          ...prev,
          {
            timestamp: new Date().toISOString(),
            stream: 'stderr',
            message: `Failed to queue build: ${err.error || res.statusText}`,
          },
        ])
        return
      }

      const data = await res.json()
      const newBuild: BuildStatusSummary = {
        build_id: data.build_id,
        status: data.status || 'running',
        duration_ms: 0,
      }
      setCurrentBuild(newBuild)

      connectSSE(data.build_id)
    } catch (err) {
      setLogs((prev) => [
        ...prev,
        {
          timestamp: new Date().toISOString(),
          stream: 'stderr',
          message: `Network error starting build: ${err}`,
        },
      ])
    }
  }

  // 8. Cancel running build
  const handleCancel = async () => {
    if (!currentBuild || !currentBuild.build_id) return

    setLogs((prev) => [
      ...prev,
      {
        timestamp: new Date().toISOString(),
        stream: 'system',
        message: '==> Signal sent to cancel build. Packer will gracefully clean up AWS instances...',
      },
    ])

    try {
      const res = await apiFetch(`/api/builds/${currentBuild.build_id}/cancel`, {
        method: 'POST',
      })
      if (!res.ok) {
        const err = await res.json().catch(() => ({}))
        setLogs((prev) => [
          ...prev,
          {
            timestamp: new Date().toISOString(),
            stream: 'stderr',
            message: `Cancel error: ${err.error || res.statusText}`,
          },
        ])
      } else {
        setCurrentBuild((prev) => (prev ? { ...prev, status: 'cancelling' } : null))
      }
    } catch (err) {
      console.error('Cancel request failed:', err)
    }
  }

  // 9. Save custom AWS Credentials
  const handleSaveAWSCredentials = async (
    accessKey: string,
    secretKey: string,
    sessionToken?: string
  ) => {
    const res = await apiFetch('/api/aws/credentials', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        access_key_id: accessKey,
        secret_access_key: secretKey,
        session_token: sessionToken,
        region: config.region,
      }),
    })

    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Failed to authenticate and save credentials')
    }

    const identity: AWSIdentityInfo = await res.json()
    setAwsStatus(identity)
    // Refetch base AMIs with new live credentials
    fetchBaseAMIs(config.region, architecture)
  }

  // 10. Clear custom AWS Credentials
  const handleClearAWSCredentials = async () => {
    const res = await apiFetch('/api/aws/credentials', {
      method: 'DELETE',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Failed to clear credentials')
    }
    await fetchAWSStatus()
    fetchBaseAMIs(config.region, architecture)
  }

  // 11. Load configuration from AMI History into Visual Builder
  const handleLoadConfigToBuilder = (loadedConfig: BuildConfig, loadedHcl?: string) => {
    setConfig(loadedConfig)
    if (loadedHcl) {
      setHclCode(loadedHcl)
      setIsManualMode(false)
    } else {
      setIsManualMode(false)
      generateHCL(loadedConfig)
    }
    setActiveTab('builder')
  }

  // Clean up EventSource on unmount
  useEffect(() => {
    return () => {
      if (activeEventSourceRef.current) {
        activeEventSourceRef.current.close()
      }
    }
  }, [])

  return (
    <div className="flex flex-col h-screen w-screen overflow-hidden bg-white text-zinc-950">
      {/* Header */}
      <Header
        health={health}
        healthLoading={healthLoading}
        healthError={healthError}
        currentBuild={currentBuild}
        awsStatus={awsStatus}
        onOpenAWSModal={() => setIsAWSModalOpen(true)}
        activeTab={activeTab}
        onTabChange={setActiveTab}
      />

      {/* Main Content Area */}
      {activeTab === 'builder' ? (
        /* Split-Screen Visual Builder & Code Terminal Workspace */
        <div className="flex-1 flex overflow-hidden">
          {/* Left Panel: Visual Builder Form */}
          <div className="w-[440px] shrink-0 h-full overflow-hidden flex flex-col bg-zinc-50 border-r border-zinc-200">
            <VisualBuilder
              config={config}
              onChange={setConfig}
              catalogue={catalogue}
              catalogueLoading={catalogueLoading}
              isManualMode={isManualMode}
              onResetToForm={handleResetToForm}
              baseAMIs={baseAMIs}
              baseAMIsLoading={baseAMIsLoading}
              baseAMISource={baseAMISource}
              architecture={architecture}
              onArchChange={(arch) => setArchitecture(arch as 'x86_64' | 'arm64')}
            />
          </div>

          {/* Right Panel: Split into Monaco Editor (Top) & Live Logs Terminal (Bottom) */}
          <div className="flex-1 flex flex-col h-full overflow-hidden border-l border-zinc-200 bg-white">
            {/* Top Half: Monaco HCL Editor */}
            <div className="flex-1 min-h-[280px] overflow-hidden bg-white">
              <HclEditor
                hclCode={hclCode}
                onChange={handleEditorChange}
                isManualMode={isManualMode}
                onResetToForm={handleResetToForm}
                onValidate={handleValidate}
                isValidating={isValidating}
                onPreflight={handlePreflightCheck}
                isPreflighting={isPreflighting}
                onBuild={handleBuild}
                onCancel={handleCancel}
                currentBuild={currentBuild}
                onSyncToForm={handleSyncToForm}
                isSyncingToForm={isSyncingToForm}
                syncNotification={syncNotification}
                onClearNotification={() => setSyncNotification(null)}
              />
            </div>

            {/* Bottom Half: Live Terminal Logs */}
            <div className="h-[340px] shrink-0 overflow-hidden bg-zinc-950 border-t border-zinc-200">
              <TerminalLogs
                logs={logs}
                onClear={() => setLogs([])}
                currentBuild={currentBuild}
              />
            </div>
          </div>
        </div>
      ) : (
        /* AMI Dashboard & Build History Tab */
        <div className="flex-1 overflow-hidden bg-white">
          <AmiDashboard
            region={config.region}
            onLoadConfigToBuilder={handleLoadConfigToBuilder}
          />
        </div>
      )}

      {/* AWS Connection & Security Modal */}
      <AWSModal
        isOpen={isAWSModalOpen}
        onClose={() => setIsAWSModalOpen(false)}
        awsStatus={awsStatus}
        onRefreshStatus={fetchAWSStatus}
        onSaveCredentials={handleSaveAWSCredentials}
        onClearCredentials={handleClearAWSCredentials}
      />
    </div>
  )
}

export default App

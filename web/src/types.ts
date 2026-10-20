export interface BuildConfig {
  build_id?: string
  region: string
  instance_type: string
  source_ami: string
  ssh_username: string
  ami_name: string
  packages: string[]
  custom_script?: string[]
}

export interface CataloguePackage {
  id: string
  name: string
  description: string
  commands: string[]
}

export interface LogEntry {
  timestamp: string
  stream: 'stdout' | 'stderr' | 'system'
  message: string
}

export interface BuildStatusSummary {
  build_id: string
  status: 'idle' | 'pending' | 'running' | 'success' | 'failed' | 'cancelled' | 'cancelling'
  ami_id?: string
  region_to_ami?: Record<string, string>
  duration_ms: number
  created_at?: string
  finished_at?: string
  error?: string
  work_dir?: string
}

export interface HealthResponse {
  status: string
  time: string
  packer_status: string
  packer_ver: string
}

export interface HCLValidateResponse {
  valid: boolean
  stage?: string
  error?: string
  logs: LogEntry[]
}

export interface AWSIdentityInfo {
  connected: boolean
  account_id?: string
  arn?: string
  user_id?: string
  source: 'encrypted_store' | 'env/profile' | 'none'
  masked_access_key?: string
  region?: string
  error?: string
}

export interface BaseAMI {
  id: string
  name: string
  display_name: string
  os: string
  family: string
  architecture: string
  default_user: string
  creation_date?: string
  description: string
  owner_id: string
}

export interface BaseAMIResponse {
  region: string
  arch: string
  source: 'aws_live' | 'fallback_preset'
  images: BaseAMI[]
}

export interface VIBImageItem {
  build_id?: string
  ami_id: string
  name: string
  region: string
  status: string
  creation_date: string
  duration_seconds?: number
  live_in_aws: boolean
  has_config: boolean
  snapshot_ids?: string[]
  tags?: Record<string, string>
}

export interface ImageConfigResponse {
  build_id: string
  ami_id: string
  ami_name: string
  region: string
  status: string
  hcl_code: string
  config_json: string
  config?: BuildConfig
  duration_seconds: number
  created_at: string
  updated_at: string
}

export interface HCLParseResponse {
  config: BuildConfig
}


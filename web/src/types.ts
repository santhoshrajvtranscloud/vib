export interface BuildConfig {
  build_id?: string
  region: string
  instance_type: string
  source_ami: string
  ssh_username: string
  ami_name: string
  packages: string[]
  custom_script?: string[]
  vpc_id?: string
  subnet_id?: string
}

export interface VPCInfo {
  vpc_id: string
  name: string
  is_default: boolean
  cidr: string
}

export interface SubnetInfo {
  subnet_id: string
  vpc_id: string
  availability_zone: string
  name: string
  public_ip_on_launch: boolean
  available_ip_count?: number
}

export interface NetworksResponse {
  region: string
  vpcs: VPCInfo[]
  subnets: SubnetInfo[]
}

export interface PreflightItem {
  name: string
  title: string
  passed: boolean
  message: string
  details?: string
}

export interface PreflightReport {
  region: string
  passed: boolean
  checks: PreflightItem[]
  timestamp: string
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
  ami_name?: string
  region?: string
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
  source: 'aws_live' | 'not_connected' | string
  images: BaseAMI[]
  error?: string
}

export interface VIBImageItem {
  build_id?: string
  ami_id: string
  name: string
  owner_id?: string
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


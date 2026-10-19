export type RoleType = '' | '*' | 'admin' | 'user' | 'member' | 'team_admin' | 'team_owner' | 'super_admin';
export interface UserState {
  id?: string;
  timezone?: string;
  superAdmin?: boolean;
  name?: string;
  avatar?: string;
  job?: string;
  organization?: string;
  location?: string;
  email?: string;
  introduction?: string;
  personalWebsite?: string;
  jobName?: string;
  organizationName?: string;
  locationName?: string;
  phone?: string;
  registrationDate?: string;
  accountId?: string;
  certification?: number;
  role: RoleType;
}

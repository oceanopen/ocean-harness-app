// doctor 域对外 API（唯一入口）。
// 消费方只从此处 import hooks/keys，域内部重构不波及消费方。
export { doctorKeys } from './keys';
export { isDoctorBusy, useDoctorCheck, useDoctorEntry, useDoctorReports } from './queries';

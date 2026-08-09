DROP TRIGGER instance_state_default_password_no_reenable;
CREATE TRIGGER instance_state_default_password_no_reenable
BEFORE UPDATE OF test_default_password_active ON instance_state
WHEN OLD.state='COMPLETED' AND OLD.test_default_password_active=0 AND NEW.test_default_password_active=1
BEGIN SELECT RAISE(ABORT, 'test default password cannot be re-enabled'); END;

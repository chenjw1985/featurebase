import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import CardHeader from '@material-ui/core/CardHeader';

import { ReactComponent as MLogo } from 'assets/m-bug-alt.svg';
import css from './AuthFlow.module.scss';
import SignInButton from './SignInButton';

function Signin(props) {
  const renderLoginForm = () => (
    <Card>
      <CardHeader subheader={'Sign in to continue'} />
      <CardContent>
        <SignInButton />
      </CardContent>
    </Card>
  );

  return (
    <div className={css.main}>
      <div className={css.loginForm}>
        <div className={css.logoContainer}>
          <MLogo className={css.logo} />
        </div>
        {renderLoginForm()}
      </div>
    </div>
  );
}
export default Signin;

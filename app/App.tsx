import { StatusBar } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { PlannerScreen } from './src/screens/PlannerScreen';

function App() {
  return (
    <SafeAreaProvider>
      <StatusBar barStyle="dark-content" />
      <PlannerScreen />
    </SafeAreaProvider>
  );
}

export default App;

// Copyright 2021 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

import { withRouter } from "react-router-dom";
import { connect } from "react-redux";
import { Dispatch } from "redux";
import {
  StatementDetails,
  StatementDetailsDispatchProps,
  StatementDetailsProps,
} from "./statementDetails";
import { AppState } from "../store";
import {
  selectStatement,
  selectStatementDetailsUiConfig,
} from "./statementDetails.selectors";
import { selectIsTenant } from "../store/uiConfig";
import {
  nodeDisplayNameByIDSelector,
  nodeRegionsByIDSelector,
} from "../store/nodes";
import { actions as sqlStatsActions } from "src/store/sqlStats";
import {
  actions as statementDiagnosticsActions,
  selectDiagnosticsReportsByStatementFingerprint,
} from "src/store/statementDiagnostics";
import { actions as analyticsActions } from "src/store/analytics";
import { actions as localStorageActions } from "src/store/localStorage";
import { actions as nodesActions } from "../store/nodes";
import { actions as nodeLivenessActions } from "../store/liveness";
import { selectTimeScale } from "../statementsPage/statementsPage.selectors";

// For tenant cases, we don't show information about node, regions and
// diagnostics.
const mapStateToProps = (state: AppState, props: StatementDetailsProps) => {
  const statement = selectStatement(state, props);
  const statementFingerprint = statement?.statement;
  return {
    statement,
    statementsError: state.adminUI.sqlStats.lastError,
    timeScale: selectTimeScale(state),
    nodeNames: selectIsTenant(state) ? {} : nodeDisplayNameByIDSelector(state),
    nodeRegions: selectIsTenant(state) ? {} : nodeRegionsByIDSelector(state),
    diagnosticsReports: selectIsTenant(state)
      ? []
      : selectDiagnosticsReportsByStatementFingerprint(
          state,
          statementFingerprint,
        ),
    uiConfig: selectStatementDetailsUiConfig(state),
    isTenant: selectIsTenant(state),
  };
};

const mapDispatchToProps = (
  dispatch: Dispatch,
): StatementDetailsDispatchProps => ({
  refreshStatements: () => dispatch(sqlStatsActions.refresh()),
  refreshStatementDiagnosticsRequests: () =>
    dispatch(statementDiagnosticsActions.refresh()),
  refreshNodes: () => dispatch(nodesActions.refresh()),
  refreshNodesLiveness: () => dispatch(nodeLivenessActions.refresh()),
  dismissStatementDiagnosticsAlertMessage: () =>
    dispatch(
      localStorageActions.update({
        key: "adminUi/showDiagnosticsModal",
        value: false,
      }),
    ),
  createStatementDiagnosticsReport: (statementFingerprint: string) => {
    dispatch(statementDiagnosticsActions.createReport(statementFingerprint));
    dispatch(
      analyticsActions.track({
        name: "Statement Diagnostics Clicked",
        page: "Statement Details",
        action: "Activated",
      }),
    );
  },
  onTabChanged: tabName =>
    dispatch(
      analyticsActions.track({
        name: "Tab Changed",
        page: "Statement Details",
        tabName,
      }),
    ),
  onDiagnosticBundleDownload: () =>
    dispatch(
      analyticsActions.track({
        name: "Statement Diagnostics Clicked",
        page: "Statement Details",
        action: "Downloaded",
      }),
    ),
  onSortingChange: (tableName, columnName) =>
    dispatch(
      analyticsActions.track({
        name: "Column Sorted",
        page: "Statement Details",
        columnName,
        tableName,
      }),
    ),
  onBackToStatementsClick: () =>
    dispatch(
      analyticsActions.track({
        name: "Back Clicked",
        page: "Statement Details",
      }),
    ),
});

export const ConnectedStatementDetailsPage = withRouter<any, any>(
  connect(mapStateToProps, mapDispatchToProps)(StatementDetails),
);
